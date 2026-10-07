package domain

import "testing"

func TestNativePolicyLegacyCompatibilityWithoutFoldingOtherPolicies(t *testing.T) {
	t.Parallel()
	for _, context := range []string{"", LegacyNativeContextPolicy, NativeContextPolicy} {
		for _, config := range []string{"", NativeConfigJSON, `{"cache_policy":"ollama-calibrated","reasoning_policy":"disabled-when-capable"}`, `{"cache_policy":"ollama-calibrated","reasoning_policy":"disabled-when-capable","skip_model_discovery":true}`} {
			if !IsNativePolicy(context, config) {
				t.Errorf("native policy split: %s %s", context, config)
			}
		}
	}
	for _, config := range []string{`{}`, `{"cache_policy":"different","reasoning_policy":"disabled-when-capable"}`, `{"cache_policy":"ollama-calibrated","reasoning_policy":"enabled"}`, `null`, `{"cache_policy":"ollama-calibrated","reasoning_policy":"disabled-when-capable","omit_seed":true}`} {
		if IsNativePolicy(NativeContextPolicy, config) {
			t.Errorf("folded different policy %s", config)
		}
	}
	if IsNativePolicy("client-limited", NativeConfigJSON) {
		t.Fatal("folded different context enforcement")
	}
	if (Session{Backend: BackendOpenAI}).Protocol() != "unknown" || (Session{Backend: BackendOpenAI}).Native() {
		t.Fatal("external session fabricated native protocol")
	}
	if !(Session{}).Native() {
		t.Fatal("legacy native session lost historical defaults")
	}
}

func TestSessionModelDisplayPreservesScope(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"20261005T140212Z-A1B2C3", "20261005T140212Z-D4E5F6"} {
		model := Model{Identity: "session:" + id + ":any-team/llama3:8b-instruct", IdentitySource: "session"}
		if label := model.ShortDigest(); label == "8b-instruct" || label == "" {
			t.Fatalf("session scope hidden: %q", label)
		}
	}
	a := Model{Identity: "session:s1:alias", IdentitySource: "session"}
	b := Model{Identity: "session:s2:alias", IdentitySource: "session"}
	if a.ShortDigest() == b.ShortDigest() {
		t.Fatal("session labels indistinguishable")
	}
	if got := (Model{Identity: "openai:revision:hash", IdentitySource: "operator-revision", Revision: "weight-revision-123"}).ShortDigest(); got != "weight-revision-123" {
		t.Errorf("revision label=%q", got)
	}
}
