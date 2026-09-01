package auth

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeProfile(t *testing.T) {
	tests := []struct {
		name        string
		displayName string
		avatarKey   string
		wantName    string
		wantErr     bool
	}{
		{name: "valid", displayName: "  Lester User  ", avatarKey: "ocean", wantName: "Lester User"},
		{name: "empty name", displayName: "  ", avatarKey: "forest", wantErr: true},
		{name: "unknown avatar", displayName: "Lester", avatarKey: "custom", wantErr: true},
		{name: "long unicode name", displayName: strings.Repeat("名", 61), avatarKey: "forest", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name, _, err := normalizeProfile(test.displayName, test.avatarKey)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeProfile() error = %v, wantErr %v", err, test.wantErr)
			}
			if name != test.wantName {
				t.Fatalf("normalizeProfile() name = %q, want %q", name, test.wantName)
			}
		})
	}
}

func TestSessionCookieSecureConfiguration(t *testing.T) {
	service := New(nil, nil, time.Hour, true)
	session, err := newSession(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	service.setCookie(recorder, session)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("session cookie = %#v", cookies)
	}
}
