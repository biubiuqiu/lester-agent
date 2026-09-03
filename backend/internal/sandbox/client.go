package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: http.DefaultClient}
}
func (c *Client) Create(ctx context.Context, id string) (*Sandbox, error) {
	var result Sandbox
	err := c.json(ctx, "POST", "/v1/sandboxes", CreateOptions{ID: id}, &result)
	return &result, err
}
func (c *Client) Inspect(ctx context.Context, id string) (*Sandbox, error) {
	var result Sandbox
	request, err := c.request(ctx, "GET", c.BaseURL+"/v1/sandboxes/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return &Sandbox{ID: id, Status: "missing"}, nil
	}
	if response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return nil, fmt.Errorf("sandbox service %s: %s", response.Status, string(data))
	}
	err = json.NewDecoder(response.Body).Decode(&result)
	return &result, err
}
func (c *Client) Exec(ctx context.Context, id string, cmd Command) (*CommandResult, error) {
	var result CommandResult
	err := c.json(ctx, "POST", "/v1/sandboxes/"+url.PathEscape(id)+"/exec", cmd, &result)
	return &result, err
}
func (c *Client) ListFiles(ctx context.Context, id, workDir, path string) ([]FileEntry, error) {
	var result struct{ Files []FileEntry }
	err := c.json(ctx, "GET", "/v1/sandboxes/"+url.PathEscape(id)+"/files?work_dir="+url.QueryEscape(workDir)+"&path="+url.QueryEscape(path), nil, &result)
	return result.Files, err
}
func (c *Client) ReadFile(ctx context.Context, id, workDir, path string) ([]byte, error) {
	request, err := c.request(ctx, "GET", c.BaseURL+"/v1/sandboxes/"+url.PathEscape(id)+"/files/content?work_dir="+url.QueryEscape(workDir)+"&path="+url.QueryEscape(path), nil)
	if err != nil {
		return nil, err
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("sandbox service: %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (25<<20)+1))
	if err == nil && len(data) > 25<<20 {
		return nil, fmt.Errorf("sandbox file exceeds the 25 MiB read limit")
	}
	return data, err
}
func (c *Client) ReadFileLines(ctx context.Context, id, workDir, path string, offset, limit int) (*FileLines, error) {
	var result FileLines
	endpoint := "/v1/sandboxes/" + url.PathEscape(id) + "/files/lines?work_dir=" + url.QueryEscape(workDir) + "&path=" + url.QueryEscape(path) + "&offset=" + fmt.Sprint(offset) + "&limit=" + fmt.Sprint(limit)
	if err := c.json(ctx, "GET", endpoint, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
func (c *Client) WriteFile(ctx context.Context, id, workDir, path string, data []byte) error {
	request, err := c.request(ctx, "PUT", c.BaseURL+"/v1/sandboxes/"+url.PathEscape(id)+"/files/content?work_dir="+url.QueryEscape(workDir)+"&path="+url.QueryEscape(path), bytes.NewReader(data))
	if err != nil {
		return err
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return fmt.Errorf("sandbox service: %s", response.Status)
	}
	return nil
}
func (c *Client) EditFile(ctx context.Context, id, workDir, path string, input FileEditRequest) (*FileEditResult, error) {
	var result FileEditResult
	endpoint := "/v1/sandboxes/" + url.PathEscape(id) + "/files/content?work_dir=" + url.QueryEscape(workDir) + "&path=" + url.QueryEscape(path)
	if err := c.json(ctx, "PATCH", endpoint, input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
func (c *Client) Action(ctx context.Context, id, action string) error {
	return c.json(ctx, "POST", "/v1/sandboxes/"+url.PathEscape(id)+"/"+action, nil, nil)
}
func (c *Client) json(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, _ := json.Marshal(input)
		body = bytes.NewReader(data)
	}
	request, err := c.request(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return fmt.Errorf("sandbox service %s: %s", response.Status, string(data))
	}
	if output != nil {
		return json.NewDecoder(response.Body).Decode(output)
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err == nil && c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return request, err
}
