package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func Error(w http.ResponseWriter, status int, err error) {
	JSON(w, status, map[string]string{"error": err.Error()})
}
func Decode(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(target); err != nil {
		Error(w, 400, err)
		return false
	}
	return true
}

func CORS(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,X-Lester-Timezone")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,PUT,DELETE,OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(204)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// API documents, including private generated previews, never request microphone
// access. The authenticated Web application sets its own narrow page policy.
func DenyMicrophone(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Permissions-Policy", "microphone=()")
		next.ServeHTTP(w, r)
	})
}
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					log.Error("panic", "value", v, "stack", string(debug.Stack()))
					Error(w, 500, errInternal{})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
func AccessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			if !strings.HasSuffix(r.URL.Path, "/events") {
				log.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
			}
		})
	}
}

type errInternal struct{}

func (errInternal) Error() string { return "internal server error" }
