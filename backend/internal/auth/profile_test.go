package auth

import (
	"strings"
	"testing"
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
