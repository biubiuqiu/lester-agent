package auth

import (
	"bufio"
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProviderVerifiedEmails(t *testing.T) {
	cases := []struct {
		name, provider, user, emails, want string
		invalid                            bool
	}{
		{"Google verified", "google", `{"sub":"g1","email":" User@Example.test ","email_verified":true}`, "", "user@example.test", false},
		{"Google unverified", "google", `{"sub":"g1","email":"user@example.test","email_verified":false}`, "", "", true},
		{"Google missing subject", "google", `{"email":"user@example.test","email_verified":true}`, "", "", true},
		{"GitHub private email", "github", `{"id":42,"login":"octocat","email":null}`, `[{"email":"unverified@example.test","primary":true,"verified":false},{"email":"verified@example.test","primary":false,"verified":true}]`, "verified@example.test", false},
		{"GitHub primary", "github", `{"id":42,"login":"octocat"}`, `[{"email":"secondary@example.test","primary":false,"verified":true},{"email":"primary@example.test","primary":true,"verified":true}]`, "primary@example.test", false},
		{"GitHub unverified", "github", `{"id":42,"login":"octocat"}`, `[{"email":"unverified@example.test","primary":true,"verified":false}]`, "", true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					http.Error(w, "bad token", 401)
					return
				}
				if r.URL.Path == "/emails" {
					io.WriteString(w, test.emails)
				} else {
					io.WriteString(w, test.user)
				}
			}))
			defer provider.Close()
			identity, err := fetchProviderIdentity(context.Background(), provider.Client(), test.provider, OAuthProvider{UserInfoURL: provider.URL + "/user", EmailsURL: provider.URL + "/emails"}, "test-token")
			if (err != nil) != test.invalid || (!test.invalid && identity.Email != test.want) {
				t.Fatalf("identity=%#v err=%v", identity, err)
			}
		})
	}
}
func TestMutationOriginProtection(t *testing.T) {
	for _, test := range []struct {
		method, origin, fetch string
		status                int
	}{{"POST", "https://lester.example.test", "same-origin", 204}, {"POST", "https://evil.example.test", "same-site", 403}, {"PATCH", "null", "cross-site", 403}, {"DELETE", "", "cross-site", 403}, {"POST", "", "", 204}, {"GET", "https://accounts.google.com", "cross-site", 204}} {
		r := httptest.NewRequest(test.method, "/", nil)
		r.Header.Set("Origin", test.origin)
		r.Header.Set("Sec-Fetch-Site", test.fetch)
		w := httptest.NewRecorder()
		MutationOrigin("https://lester.example.test")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%#v: %d", test, w.Code)
		}
	}
}
func TestAuthConfigurationAndAvatarURLAllowlist(t *testing.T) {
	for _, origin := range []string{"https://example.test/path", "https://user:pass@example.test", "https://example.test?redirect=evil", "https://example.test#fragment", "//example.test"} {
		if New(nil, nil, time.Hour, true).Configure(Options{WebOrigin: origin}) == nil {
			t.Fatalf("invalid origin: %s", origin)
		}
	}
	if New(nil, nil, time.Hour, false).Configure(Options{WebOrigin: "https://example.test"}) == nil {
		t.Fatal("insecure HTTPS cookie")
	}
	if New(nil, nil, time.Hour, false).Configure(Options{WebOrigin: "http://public.example.test", Providers: DefaultProviders("test-id", "test-secret", "", "")}) == nil {
		t.Fatal("public HTTP OAuth")
	}
	for _, raw := range []string{"http://avatars.githubusercontent.com/a", "https://127.0.0.1/a", "https://avatars.githubusercontent.com.evil.test/a", "https://user@avatars.githubusercontent.com/a", "https://lh3.googleusercontent.com:444/a", "https://example.test/a"} {
		if allowedAvatarURL(raw) {
			t.Fatalf("unsafe avatar accepted: %s", raw)
		}
	}
	for _, raw := range []string{"https://avatars.githubusercontent.com/u/42?v=4", "https://lh3.googleusercontent.com/a/image=s96-c"} {
		if !allowedAvatarURL(raw) {
			t.Fatalf("valid provider rejected: %s", raw)
		}
	}
}
func TestAvatarRejectsOversizedDimensionsAndActiveContent(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 4097, 1))); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{data.Bytes(), data.Bytes()[:32], []byte(`<svg onload="alert(1)"/>`)} {
		if _, err := normalizeAvatar(raw); err == nil {
			t.Fatal("unsafe/corrupted image accepted")
		}
	}
}
func TestPasswordBoundsAndMalformedHashes(t *testing.T) {
	for _, password := range []string{"short", strings.Repeat("x", 1025), string([]byte{0xff, 0xff})} {
		if validatePassword(password) == nil {
			t.Fatal("invalid password accepted")
		}
	}
	for _, encoded := range []string{"", `$argon2id$v=19$m=999999999,t=99,p=20$YmFk$YmFk`, `$argon2id$v=19$m=0,t=0,p=0$$`} {
		if verifyPassword("test-password-123", encoded) {
			t.Fatal("malformed password accepted")
		}
	}
	if validatePassword(strings.Repeat("密", 10)) != nil {
		t.Fatal("Unicode password rejected")
	}
}
func TestSMTPConfigurationAndDelivery(t *testing.T) {
	for _, config := range []SMTPConfig{{Host: "smtp.example.test", Port: 587, From: "team@example.test", TLSMode: "plain"}, {Host: "smtp.example.test", Port: 587, From: "team@example.test\r\nBcc: victim@example.test", TLSMode: "starttls"}, {Host: "smtp.example.test", Port: 587, From: "team@example.test", TLSMode: "starttls", Username: "user"}} {
		if _, err := NewSMTPMailer(config); err == nil {
			t.Fatalf("invalid SMTP config allowed: %#v", config)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	delivered := make(chan string, 1)
	go func() {
		connection, e := listener.Accept()
		if e != nil {
			return
		}
		defer connection.Close()
		connection.SetDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(connection)
		io.WriteString(connection, "220 localhost SMTP\r\n")
		for {
			line, e := reader.ReadString('\n')
			if e != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				io.WriteString(connection, "250-localhost\r\n250 SIZE 1000000\r\n")
			case strings.HasPrefix(line, "DATA"):
				io.WriteString(connection, "354 Send data\r\n")
				var body strings.Builder
				for {
					line, e = reader.ReadString('\n')
					if e != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
					body.WriteString(line)
				}
				delivered <- body.String()
				io.WriteString(connection, "250 Stored\r\n")
			case strings.HasPrefix(line, "QUIT"):
				io.WriteString(connection, "221 Bye\r\n")
				return
			default:
				io.WriteString(connection, "250 OK\r\n")
			}
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	mailer, err := NewSMTPMailer(SMTPConfig{Host: host, Port: port, From: "Lester <team@example.test>", TLSMode: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if err = mailer.Send(context.Background(), "user@example.test", "验证邮箱", "https://lester.example.test/login?mode=verify#token=test-token"); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-delivered:
		if !strings.Contains(body, "Content-Type: text/plain; charset=utf-8") || !strings.Contains(body, "#token=test-token") {
			t.Fatalf("invalid message: %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("mail not delivered")
	}
}

func TestSafeAuthReturn(t *testing.T) {
	for _, value := range []string{"https://evil.test/app", "//evil.test/app", "/app/../login", "/app/..", "/app/\\evil", "/preview/nope", "/docs", "/app\r\n"} {
		if got := safeAuthReturn(value); got != "/app" {
			t.Fatalf("unsafe return %q -> %q", value, got)
		}
	}
	for _, value := range []string{"/app/settings/profile", "/admin/users", "/preview/11111111-1111-1111-1111-111111111111?path=index.html"} {
		if got := safeAuthReturn(value); got != value {
			t.Fatalf("valid return %q -> %q", value, got)
		}
	}
}
