package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

type Bedrock struct{}

func (Bedrock) Name() string                          { return "bedrock" }
func (Bedrock) Protocol() string                      { return "anthropic" }
func (Bedrock) DefaultEndpoint(map[string]any) string { return "" }
func (Bedrock) NewClient(spec ClientSpec) (modelruntime.Client, error) {
	var credentials bedrockCredentials
	if json.Unmarshal(spec.Credential, &credentials) != nil || credentials.AccessKeyID == "" || credentials.SecretAccessKey == "" {
		return nil, errors.New("Bedrock credential must contain access_key_id and secret_access_key")
	}
	region := configString(spec.Config, "region")
	if region == "" {
		return nil, errors.New("Bedrock requires region")
	}
	return &bedrockClient{region: region, credentials: credentials, http: &http.Client{Timeout: 10 * time.Minute}}, nil
}

type bedrockCredentials struct {
	AccessKeyID, SecretAccessKey, SessionToken string
}

func (c *bedrockCredentials) UnmarshalJSON(data []byte) error {
	type credentials struct {
		AccessKeyID     string `json:"access_key_id"`
		SecretAccessKey string `json:"secret_access_key"`
		SessionToken    string `json:"session_token"`
	}
	var decoded credentials
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	c.AccessKeyID, c.SecretAccessKey, c.SessionToken = decoded.AccessKeyID, decoded.SecretAccessKey, decoded.SessionToken
	return nil
}

type bedrockClient struct {
	region      string
	credentials bedrockCredentials
	http        *http.Client
}

func (c *bedrockClient) Capabilities(context.Context, string) (modelruntime.Capabilities, error) {
	return modelruntime.Capabilities{Streaming: false, Tools: true, Vision: true, TokenCounting: true}, nil
}

func (c *bedrockClient) Stream(ctx context.Context, request modelruntime.Request) (<-chan modelruntime.Event, error) {
	response, err := c.Generate(ctx, request)
	if err != nil {
		return nil, err
	}
	events := make(chan modelruntime.Event, 3)
	events <- modelruntime.Event{Type: "MODEL_DELTA", Delta: response.Content}
	for index := range response.ToolCalls {
		call := response.ToolCalls[index]
		events <- modelruntime.Event{Type: "MODEL_DELTA", ToolCall: &call}
	}
	events <- modelruntime.Event{Type: "MODEL_COMPLETED", Usage: response.Usage}
	close(events)
	return events, nil
}

func (c *bedrockClient) Generate(ctx context.Context, request modelruntime.Request) (*modelruntime.Response, error) {
	messages := make([]map[string]any, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Role == "tool" {
			messages = append(messages, map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": message.ToolCallID, "content": message.Content}}})
			continue
		}
		content := []any{map[string]any{"type": "text", "text": message.Content}}
		for _, call := range message.ToolCalls {
			var input any
			_ = json.Unmarshal(call.Arguments, &input)
			content = append(content, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": input})
		}
		messages = append(messages, map[string]any{"role": message.Role, "content": content})
	}
	payload := map[string]any{"anthropic_version": "bedrock-2023-05-31", "max_tokens": max(request.MaxTokens, 4096), "system": request.System, "messages": messages}
	if len(request.Tools) > 0 {
		tools := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": tool.InputSchema})
		}
		payload["tools"] = tools
	}
	body, _ := json.Marshal(payload)
	requestPath := "/model/" + url.PathEscape(request.Model) + "/invoke"
	endpoint := "https://bedrock-runtime." + c.region + ".amazonaws.com" + requestPath
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.sign(httpRequest, body, time.Now().UTC(), requestPath)
	response, err := c.http.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("bedrock %s: %s", response.Status, string(data))
	}
	var decoded struct {
		Content []struct {
			Type, Text, ID, Name string
			Input                json.RawMessage
		}
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		}
	}
	if err = json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	result := &modelruntime.Response{Usage: map[string]int{"input_tokens": decoded.Usage.InputTokens, "output_tokens": decoded.Usage.OutputTokens}}
	for index, block := range decoded.Content {
		if block.Type == "text" {
			result.Content += block.Text
		}
		if block.Type == "tool_use" {
			result.ToolCalls = append(result.ToolCalls, modelruntime.ToolCall{ID: block.ID, Name: block.Name, Arguments: block.Input, Index: index})
		}
	}
	return result, nil
}

func (c *bedrockClient) sign(request *http.Request, body []byte, now time.Time, requestPath string) {
	payloadHash := sha256Hex(body)
	date, timestamp := now.Format("20060102"), now.Format("20060102T150405Z")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Host", request.URL.Host)
	request.Header.Set("X-Amz-Date", timestamp)
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	signedHeaders := "content-type;host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := "content-type:application/json\n" + "host:" + request.URL.Host + "\n" + "x-amz-content-sha256:" + payloadHash + "\n" + "x-amz-date:" + timestamp + "\n"
	if c.credentials.SessionToken != "" {
		request.Header.Set("X-Amz-Security-Token", c.credentials.SessionToken)
		signedHeaders += ";x-amz-security-token"
		canonicalHeaders += "x-amz-security-token:" + c.credentials.SessionToken + "\n"
	}
	canonical := request.Method + "\n" + requestPath + "\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + payloadHash
	scope := date + "/" + c.region + "/bedrock/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + timestamp + "\n" + scope + "\n" + sha256Hex([]byte(canonical))
	dateKey := hmacSHA([]byte("AWS4"+c.credentials.SecretAccessKey), date)
	regionKey := hmacSHA(dateKey, c.region)
	serviceKey := hmacSHA(regionKey, "bedrock")
	signingKey := hmacSHA(serviceKey, "aws4_request")
	signature := hex.EncodeToString(hmacSHA(signingKey, stringToSign))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.credentials.AccessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func hmacSHA(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}
func sha256Hex(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
