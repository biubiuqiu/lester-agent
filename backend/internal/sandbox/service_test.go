package sandbox

import (
	"net/http"
	"net/http/httptest"
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
