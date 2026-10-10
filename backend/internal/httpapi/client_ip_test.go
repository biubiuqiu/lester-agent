package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestClientIPTrustsOnlyConfiguredPeers(t *testing.T) {
	for _, test := range []struct {
		name, peer, forwarded, want string
		trusted                     []string
	}{
		{"direct spoof", "198.51.100.7:9000", "203.0.113.99", "198.51.100.7", nil},
		{"untrusted peer", "198.51.100.7:9000", "203.0.113.99", "198.51.100.7", []string{"10.1.0.0/24"}},
		{"gateway", "10.1.0.9:9000", "198.51.100.7", "198.51.100.7", []string{"10.1.0.0/24"}},
		{"spoofed leftmost", "10.1.0.9:9000", "203.0.113.99, 198.51.100.7, 10.1.0.8", "198.51.100.7", []string{"10.1.0.0/24"}},
		{"invalid forwarding", "10.1.0.9:9000", "203.0.113.99, invalid", "10.1.0.9", []string{"10.1.0.0/24"}},
		{"no forwarding", "10.1.0.9:9000", "", "10.1.0.9", []string{"10.1.0.0/24"}},
		{"mapped peer", "[::ffff:10.1.0.9]:9000", "198.51.100.7", "198.51.100.7", []string{"10.1.0.0/24"}},
		{"IPv6", "[2001:db8::9]:9000", "2001:db8:1::7", "2001:db8:1::7", []string{"2001:db8::/64"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = test.peer
			request.Header.Set("X-Forwarded-For", test.forwarded)
			request.Header.Set("True-Client-IP", "203.0.113.99")
			request.Header.Set("X-Real-IP", "203.0.113.99")
			ClientIP(test.trusted)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := middleware.GetClientIP(r.Context()); got != test.want {
					t.Errorf("client IP = %q, want %q", got, test.want)
				}
				if r.RemoteAddr != test.peer {
					t.Errorf("TCP peer address was rewritten")
				}
			})).ServeHTTP(httptest.NewRecorder(), request)
		})
	}
}
