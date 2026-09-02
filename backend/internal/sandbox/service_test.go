package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceRequiresTokenExceptHealth(t *testing.T) {
	handler := NewServiceHandler(NewDockerProvider(""), "0123456789abcdef0123456789abcdef").Router()

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d", health.Code)
	}

	protected := httptest.NewRecorder()
	handler.ServeHTTP(protected, httptest.NewRequest(http.MethodGet, "/v1/sandboxes/test", nil))
	if protected.Code != http.StatusUnauthorized {
		t.Fatalf("protected status = %d, want %d", protected.Code, http.StatusUnauthorized)
	}
}

func TestServiceEditFileUsesProvider(t *testing.T) {
	provider := &editFileProvider{result: &FileEditResult{OK: true, Replacements: 2, SHA256: "abc123"}}
	handler := NewServiceHandler(provider, "0123456789abcdef0123456789abcdef").Router()
	request := httptest.NewRequest(http.MethodPatch, "/v1/sandboxes/test/files/content?work_dir=%2Fworkspace%2Fconversations%2Ftest&path=notes.txt", strings.NewReader(`{"old_string":"red","new_string":"green","replace_all":true}`))
	request.Header.Set("Authorization", "Bearer 0123456789abcdef0123456789abcdef")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if provider.id != "test" || provider.workDir != "/workspace/conversations/test" || provider.path != "notes.txt" || !provider.request.ReplaceAll {
		t.Fatalf("provider call = %#v", provider)
	}
	var result FileEditResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Replacements != 2 || result.SHA256 != "abc123" {
		t.Fatalf("result = %#v, %v", result, err)
	}
}

type editFileProvider struct {
	Provider
	id, workDir, path string
	request           FileEditRequest
	result            *FileEditResult
}

func (p *editFileProvider) EditFile(_ context.Context, id, workDir, path string, request FileEditRequest) (*FileEditResult, error) {
	p.id, p.workDir, p.path, p.request = id, workDir, path, request
	return p.result, nil
}
