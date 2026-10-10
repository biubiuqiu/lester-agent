package httpapi

import (
	"net"
	"net/http"
	"net/netip"

	"github.com/go-chi/chi/v5/middleware"
)

// Only explicitly trusted TCP peers may supply X-Forwarded-For. Never trust
// True-Client-IP or X-Real-IP; preserve RemoteAddr as the actual peer address.
func ClientIP(trustedCIDRs []string) func(http.Handler) http.Handler {
	prefixes := make([]netip.Prefix, len(trustedCIDRs))
	for i, cidr := range trustedCIDRs {
		prefixes[i] = netip.MustParsePrefix(cidr)
	}
	return func(next http.Handler) http.Handler {
		forwarded := middleware.ClientIPFromXFF(trustedCIDRs...)(next)
		return middleware.ClientIPFromRemoteAddr(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			peer, err := netip.ParseAddr(host)
			if err == nil {
				for _, prefix := range prefixes {
					if prefix.Contains(peer.Unmap()) {
						forwarded.ServeHTTP(w, r)
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		}))
	}
}
