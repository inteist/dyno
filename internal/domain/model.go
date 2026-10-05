package domain

import "strings"

// Model describes one local model.
type Model struct {
	Name         string // for example "qwen3.5:9b-mlx"
	Digest       string
	Format       string // "safetensors" (MLX engine) or "gguf"
	Family       string // for example "qwen3_5" or "lfm2moe"
	Parameters   string // for example "9.4B"
	Quantization string // for example "nvfp4" or "Q4_K_M"
	SizeBytes    int64
	Thinking     bool // the model lists the "thinking" capability
}

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

// shortDigestLen is the number of digest characters that people read.
const shortDigestLen = 12

// ShortDigest returns the first characters of the digest.
func (m Model) ShortDigest() string {
	if len(m.Digest) <= shortDigestLen {
		return m.Digest
	}
	return m.Digest[:shortDigestLen]
}
