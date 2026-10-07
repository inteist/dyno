package openaicompat

import (
	"strings"
	"testing"
)

func TestCredentialCannotEnterDecodedOrEscapedProvenancePath(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{
		{"secret-key", "https://host/secret-key/v1"},
		{"section/key", "https://host/section%2Fkey/v1"},
		{"section%2Fkey", "https://host/section%2Fkey/v1"},
	} {
		client, err := New(Config{APIKey: pair[0], BaseURL: pair[1]})
		if client != nil || err == nil || strings.Contains(err.Error(), pair[0]) {
			t.Fatalf("credential-bearing path accepted or exposed: client=%v error=%v", client != nil, err)
		}
	}
}
