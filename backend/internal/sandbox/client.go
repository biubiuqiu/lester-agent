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
	HTTP    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: http.DefaultClient}
}
func (c *Client) Create(ctx context.Context, id string) (*Sandbox, error) {
	var result Sandbox
	err := c.json(ctx, "POST", "/v1/sandboxes", CreateOptions{ID: id}, &result)
	return &result, err
}
func (c *Client) Inspect(ctx context.Context, id string) (*Sandbox, error) {
	var result Sandbox
	request, _ := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/v1/sandboxes/"+id, nil)
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return &Sandbox{ID: id, Status: "missing"}, nil
	}
	if response.StatusCode >= 300 {
		data, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("sandbox service %s: %s", response.Status, string(data))
	}
	err = json.NewDecoder(response.Body).Decode(&result)
	return &result, err
}
func (c *Client) Exec(ctx context.Context, id string, cmd Command) (*CommandResult, error) {
	var result CommandResult
	err := c.json(ctx, "POST", "/v1/sandboxes/"+id+"/exec", cmd, &result)
	return &result, err
}
func (c *Client) ListFiles(ctx context.Context, id, workDir, path string) ([]FileEntry, error) {
	var result struct{ Files []FileEntry }
	err := c.json(ctx, "GET", "/v1/sandboxes/"+id+"/files?work_dir="+url.QueryEscape(workDir)+"&path="+url.QueryEscape(path), nil, &result)
	return result.Files, err
}
func (c *Client) ReadFile(ctx context.Context, id, workDir, path string) ([]byte, error) {
	request, _ := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/v1/sandboxes/"+id+"/files/content?work_dir="+url.QueryEscape(workDir)+"&path="+url.QueryEscape(path), nil)
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("sandbox service: %s", response.Status)
	}
	return io.ReadAll(response.Body)
}
func (c *Client) WriteFile(ctx context.Context, id, workDir, path string, data []byte) error {
	request, _ := http.NewRequestWithContext(ctx, "PUT", c.BaseURL+"/v1/sandboxes/"+id+"/files/content?work_dir="+url.QueryEscape(workDir)+"&path="+url.QueryEscape(path), bytes.NewReader(data))
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
func (c *Client) Action(ctx context.Context, id, action string) error {
	return c.json(ctx, "POST", "/v1/sandboxes/"+id+"/"+action, nil, nil)
}
func (c *Client) json(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, _ := json.Marshal(input)
		body = bytes.NewReader(data)
	}
	request, _ := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		data, _ := io.ReadAll(response.Body)
		return fmt.Errorf("sandbox service %s: %s", response.Status, string(data))
	}
	if output != nil {
		return json.NewDecoder(response.Body).Decode(output)
	}
	return nil
}
