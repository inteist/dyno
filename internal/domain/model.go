package domain

import "strings"

// Model describes one local model.
type Model struct {
	// MetadataAvailable is nil for legacy/native model literals (known fields).
	// Catalogs lacking these facts use an explicit mask, preserving unknown.
	MetadataAvailable *ModelMetadataAvailability
	Name              string // for example "qwen3.5:9b-mlx"
	Digest            string // known content checksum; empty for unverified aliases
	Identity          string // full storage key, independent of a content checksum
	IdentitySource    string // digest, operator-revision, or session
	Revision          string
	Format            string // "safetensors" (MLX engine) or "gguf"
	Family            string // for example "qwen3_5" or "lfm2moe"
	Parameters        string // for example "9.4B"
	Quantization      string // for example "nvfp4" or "Q4_K_M"
	SizeBytes         int64
	Thinking          bool // the model lists the "thinking" capability
}

// ModelMetadataAvailability distinguishes unavailable facts from known zero/no.
type ModelMetadataAvailability struct {
	SizeBytes, Thinking bool
}

func (m Model) HasSizeBytes() bool {
	return m.MetadataAvailable == nil || m.MetadataAvailable.SizeBytes
}
func (m Model) HasThinking() bool { return m.MetadataAvailable == nil || m.MetadataAvailable.Thinking }

// DefaultTag is the tag of a model name without a tag, as in Ollama.
const DefaultTag = "latest"

// NormalizeModelName returns name with DefaultTag when name has no tag, so
// that "lfm2.5" and "lfm2.5:latest" name the same model. Only the last path
// segment can hold the tag, because a registry host can hold a port, as in
// "localhost:5000/team/model".
func NormalizeModelName(name string) string {
	last := name[strings.LastIndex(name, "/")+1:]
	if last == "" || strings.Contains(last, ":") {
		return name
	}
	return name + ":" + DefaultTag
}

// NormalizeModelNames copies and trims the selection, then applies an optional
// provider policy. A nil policy preserves opaque served IDs.
func NormalizeModelNames(names []string, normalize func(string) string) []string {
	result := make([]string, len(names))
	for i, name := range names {
		name = strings.TrimSpace(name)
		if normalize != nil {
			name = normalize(name)
		}
		result[i] = name
	}
	return result
}

// shortDigestLen is the number of digest characters that people read.
const shortDigestLen = 12

// StorageKey returns the full identity, preserving historical Ollama digest keys.
func (m Model) StorageKey() string {
	if m.Identity != "" {
		return m.Identity
	}
	return m.Digest
}

// ShortDigest displays a native digest, short session scope, or operator revision.
// Grouping and persistence must use the full StorageKey, never this label.
func (m Model) ShortDigest() string {
	key := m.StorageKey()
	if m.IdentitySource == "operator-revision" && m.Revision != "" {
		hash := strings.TrimPrefix(m.Revision, "sha256:")
		if (len(hash) == 40 || len(hash) == 64) && (strings.HasPrefix(m.Revision, "sha256:") || strings.ContainsAny(hash, "abcdefABCDEF")) && !strings.ContainsFunc(hash, func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') }) {
			return hash[:shortDigestLen] + "…"
		}
		return m.Revision
	}
	if m.IdentitySource == "session" || strings.HasPrefix(key, "session:") || strings.HasPrefix(key, "openai:session:") {
		scope := strings.TrimPrefix(strings.TrimPrefix(key, "openai:"), "session:")
		id, _, _ := strings.Cut(scope, ":")
		if i := strings.LastIndex(id, "-"); i >= 0 {
			id = id[i+1:]
		}
		return "session-" + id[:min(shortDigestLen, len(id))]
	}
	if m.Digest == "" {
		if i := strings.LastIndex(key, ":"); i >= 0 {
			key = key[i+1:]
		}
	}
	if len(key) <= shortDigestLen {
		return key
	}
	return key[:shortDigestLen]
}
