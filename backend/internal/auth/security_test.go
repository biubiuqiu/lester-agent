package auth

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestUnavailableRateLimitStoreDoesNotBypassLimits(t *testing.T) {
	client := redis.NewClient(&redis.Options{MaxRetries: -1, Dialer: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("fixture rate limit store unavailable")
	}})
	defer client.Close()
	s := &Service{redis: client}
	if s.allowAttempt(httptest.NewRequest("POST", "/login", nil), "login", "fixture@example.test", 20) {
		t.Fatal("unavailable Redis bypassed authentication rate limiting")
	}
}
