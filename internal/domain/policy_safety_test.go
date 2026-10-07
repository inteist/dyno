package domain

import "testing"

func TestUnknownPolicyKeysAreConservativelyComparable(t *testing.T) {
	a, _ := ComparableConfigJSON(`{"usage":"required"}`)
	b, _ := ComparableConfigJSON(`{"usage":"required","enable_thinking":false}`)
	if a == b {
		t.Fatal("unknown measurement setting merged")
	}
	if IsNativePolicy(NativeContextPolicy, `{"reasoning_policy":"disabled-when-capable","cache_policy":"ollama-calibrated","enable_thinking":false}`) {
		t.Fatal("unknown native setting merged")
	}
}

func TestTimestampRevisionsAreNotDigestPrefixes(t *testing.T) {
	for _, revision := range []string{"20261006123456", "20261006123499"} {
		if got := (Model{IdentitySource: "operator-revision", Revision: revision}).ShortDigest(); got != revision {
			t.Fatal(got)
		}
	}
}
