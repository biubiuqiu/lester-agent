package config

import "testing"

func TestTrustedProxyCIDRsRejectUnsafeConfiguration(t *testing.T) {
	for _, raw := range []string{"not-an-ip", "0.0.0.0/0", "::/0", "::ffff:10.1.0.0/120"} {
		if _, err := parseTrustedProxyCIDRs(raw); err == nil {
			t.Errorf("accepted unsafe proxy configuration %q", raw)
		}
	}
	got, err := parseTrustedProxyCIDRs(" 10.1.0.9/24,2001:db8::/64 ")
	if err != nil || len(got) != 2 || got[0] != "10.1.0.0/24" {
		t.Fatalf("prefixes = %v, %v", got, err)
	}
	if got, err := parseTrustedProxyCIDRs(""); err != nil || len(got) != 0 {
		t.Fatalf("empty configuration = %v, %v", got, err)
	}
}
