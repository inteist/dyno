package openaicompat

import (
	"context"
	"dyno/internal/domain"
	"encoding/json"
	"slices"
	"testing"
)

func TestAdapterPolicyKeysAreExplicitlyClassified(t *testing.T) {
	c := newTestClient(t, Config{})
	info, err := c.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(info.ConfigJSON), &fields); err != nil {
		t.Fatal(err)
	}
	for k := range fields {
		comparable := slices.Contains(domain.ComparableConfigKeys(), k)
		operational := slices.Contains(domain.OperationalConfigKeys(), k)
		if comparable == operational {
			t.Fatalf("adapter key %q must be in exactly one policy-key contract list", k)
		}
	}
}

func TestInvalidUTF8CredentialRejectedWithoutPanic(t *testing.T) {
	if c, err := New(Config{APIKey: string([]byte{255, 254})}); c != nil || err == nil {
		t.Fatalf("invalid credential accepted: %v", err)
	}
}
