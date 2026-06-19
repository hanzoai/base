package auth

import "testing"

func TestNetworkFromRedirectURI(t *testing.T) {
	cases := map[string]string{
		"https://cloud.lux.network/auth/callback":  "lux",
		"https://cloud.hanzo.ai/auth/callback":     "hanzo",
		"https://cloud.zoo.network/auth/callback":  "zoo",
		"https://cloud.pars.network/auth/callback": "pars",
		"https://bootno.de/login":                  "lux",
		"https://example.com/cb":                   "",
		"not a url at all":                         "",
		"":                                         "",
	}
	for uri, want := range cases {
		if got := NetworkFromRedirectURI(uri); got != want {
			t.Errorf("NetworkFromRedirectURI(%q) = %q, want %q", uri, got, want)
		}
	}
}

func TestClientIDForRedirect(t *testing.T) {
	if got := ClientIDForRedirect("https://cloud.zoo.network/cb", "fallback"); got != "lux-web3" {
		t.Errorf("zoo network must map to lux-web3, got %q", got)
	}
	if got := ClientIDForRedirect("https://unknown.example/cb", "fallback"); got != "fallback" {
		t.Errorf("unknown redirect must fall back, got %q", got)
	}
}

func TestClassifyCredential(t *testing.T) {
	cases := []struct {
		cred     string
		isBearer bool
		want     KeyType
	}{
		{"bn_abc123", false, KeyBootnode},
		{"pk-abc", false, KeyIAMPublishable},
		{"sk-abc", false, KeyIAMSecret},
		{"hk-abc", false, KeyIAMService},
		{"aaa.bbb.ccc", false, KeyJWT},
		{"opaque", true, KeyJWT}, // bearer header => treat as JWT
		{"opaque", false, KeyUnknown},
	}
	for _, c := range cases {
		if got := ClassifyCredential(c.cred, c.isBearer); got != c.want {
			t.Errorf("ClassifyCredential(%q, %v) = %d, want %d", c.cred, c.isBearer, got, c.want)
		}
	}
}

func TestGenerateAndVerifyKey(t *testing.T) {
	salt := "test-salt"
	raw, hash, prefix, err := GenerateKey(salt)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if len(raw) < len(BootnodeKeyPrefix)+16 {
		t.Fatalf("raw key too short: %q", raw)
	}
	if raw[:3] != BootnodeKeyPrefix {
		t.Fatalf("raw key missing bn_ prefix: %q", raw)
	}
	if len(prefix) != keyPrefixLen || prefix != raw[:keyPrefixLen] {
		t.Fatalf("prefix mismatch: %q", prefix)
	}
	if !VerifyKey(raw, salt, hash) {
		t.Fatal("VerifyKey must accept the freshly-generated key")
	}
	// Tamper: a single different character must not verify.
	if VerifyKey(raw+"x", salt, hash) {
		t.Fatal("VerifyKey must reject a tampered key")
	}
	if VerifyKey(raw, "wrong-salt", hash) {
		t.Fatal("VerifyKey must reject a key checked with the wrong salt")
	}
}

func TestHashKeyDeterministic(t *testing.T) {
	// The hash is a lookup index, so it MUST be deterministic.
	a := HashKey("bn_xyz", "salt")
	b := HashKey("bn_xyz", "salt")
	if a != b {
		t.Fatal("HashKey must be deterministic for the same input")
	}
	if HashKey("bn_xyz", "salt") == HashKey("bn_xyz", "other") {
		t.Fatal("different salts must yield different hashes")
	}
	// Two distinct keys must not collide.
	r1, h1, _, _ := GenerateKey("s")
	r2, h2, _, _ := GenerateKey("s")
	if r1 == r2 || h1 == h2 {
		t.Fatal("distinct generated keys must not collide")
	}
}
