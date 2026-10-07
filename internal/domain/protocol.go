package domain

import (
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
)

// Backend and measurement vocabulary shared by adapters, persistence and views.
const (
	BackendOllama             = "ollama"
	BackendOpenAI             = "openai"
	ProtocolOllama            = "ollama-server-v1"
	ProtocolOpenAIChat        = "openai-chat-v1"
	LegacyNativeContextPolicy = "request"
	NativeContextPolicy       = "request-controlled"
	ReasoningPolicyKey        = "reasoning_policy"
	TTFTBasisKey              = "ttft_basis"
	TTFTBasisVisibleContent   = "first-nonempty-content"
	NativeConfigJSON          = `{"reasoning_policy":"disabled-when-capable","cache_policy":"ollama-calibrated"}`
)

// Native reports the historical Ollama measurement protocol, not merely a name.
func (s Session) Native() bool {
	return s.BackendName() == BackendOllama && s.Protocol() == ProtocolOllama
}

// ComparableConfigKeys and OperationalConfigKeys return independent contract
// lists classifying adapter-written keys; callers cannot mutate policy behavior.
func ComparableConfigKeys() []string {
	return []string{ReasoningPolicyKey, TTFTBasisKey, "cache_policy", "usage", "token_limit_field", "omit_seed", "omit_temperature", "completion_tokens_policy"}
}
func OperationalConfigKeys() []string { return []string{"skip_model_discovery"} }

// ComparableConfigJSON retains all keys except explicitly operational metadata.
// Unknown keys are fail-closed: a new setting splits history until classified.
func ComparableConfigJSON(raw string) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return "", errors.New("trailing configuration JSON")
	}
	if config, ok := value.(map[string]any); ok {
		selected := make(map[string]any)
		operational := OperationalConfigKeys()
		for key, v := range config {
			// Unknown keys are conservative: never silently merge a new setting.
			if !slices.Contains(operational, key) {
				selected[key] = v
			}
		}
		value = selected
	}
	canonical, err := json.Marshal(value)
	return string(canonical), err
}

// IsNativePolicy recognizes metadata spelling changes for the existing native
// algorithm. It never folds an actually different reasoning/cache/context policy.
func IsNativePolicy(contextPolicy, configJSON string) bool {
	if contextPolicy != "" && contextPolicy != LegacyNativeContextPolicy && contextPolicy != NativeContextPolicy {
		return false
	}
	if configJSON == "" {
		return true
	}
	actual, err := ComparableConfigJSON(configJSON)
	return err == nil && actual == `{"cache_policy":"ollama-calibrated","reasoning_policy":"disabled-when-capable"}`
}
