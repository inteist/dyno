package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestCredentialEncodingAndRevisionLabels(t *testing.T) {
	_, _, err := parse(testEnv(t, map[string]string{"OPENAI_API_KEY": string([]byte{255, 254})}), "models", "--backend", "openai")
	if !errors.Is(err, ErrUsage) {
		t.Fatal("invalid credential accepted", err)
	}
	for _, revision := range []string{"r1 ", " r1", "    "} {
		_, _, err := parse(testEnv(t, nil), "run", "--backend", "openai", "--models", "m", "--model-revision", revision)
		if !errors.Is(err, ErrUsage) {
			t.Fatal("padded revision accepted", revision, err)
		}
	}
	_, _, err = parse(testEnv(t, nil), "run", "--backend", "openai", "--models", "m", "--model-revision", "sha256:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = parse(testEnv(t, nil), "run", "--models", "native", "--tag", strings.Repeat("a", 70))
	if err != nil {
		t.Fatal("native tag length regressed", err)
	}
}
