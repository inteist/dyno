package endpoint

import "testing"

func TestParseCanonicalAndSecretSafe(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{
		{"HTTP://LOCALHOST:080/v1/", "http://localhost/v1"},
		{"https://Example.COM:443/custom/api", "https://example.com/custom/api"},
		{"http://[::1]:8000/v1/", "http://[::1]:8000/v1"},
		{"http://host/llm%2Fname/v1/", "http://host/llm%2Fname/v1"},
		{"http://host/ending%2F", "http://host/ending%2F"},
	} {
		u, err := Parse(pair[0])
		if err != nil || u.String() != pair[1] {
			t.Errorf("parse %q = %v %v, want %q", pair[0], u, err, pair[1])
		}
	}
	for _, raw := range []string{"http://host:abc/v1", "http://host:0/v1", "http://host:65536/v1", "http://host:/v1", "http://[bad::address]/v1", "http://user:password@host/v1", "http://host/v1?key=secret", "http://host/v1#secret", "http://host/v1?", "http://host/%00/v1", "http://host/\x7f/v1", "relative/v1"} {
		if u, err := Parse(raw); err == nil {
			t.Errorf("accepted malformed %q: %v", raw, u)
		}
	}
}

func TestLoopbackClassification(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"localhost", "127.0.0.1", "[::1]", "[::ffff:127.0.0.1]", "remote.example"} {
		u, err := Parse("https://" + host + "/v1")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := IsLoopback(u), host != "remote.example"; got != want {
			t.Errorf("%s loopback=%v", host, got)
		}
	}
}
