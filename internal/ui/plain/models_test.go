package plain

import (
	"bytes"
	"errors"
	"testing"

	"token-rate/internal/domain"
	"token-rate/internal/golden"
)

func sampleModels() []domain.Model {
	return []domain.Model{
		{
			Name: "qwen3.5:9b-mlx", Digest: "c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2",
			Format: "safetensors", Family: "qwen3_5", Parameters: "9.4B", Quantization: "nvfp4",
			SizeBytes: 5_600_000_000, Thinking: true,
		},
		{
			Name: qwen, Digest: digestOf(qwen),
			Format: "safetensors", Family: "qwen3_5", Parameters: "4.7B", Quantization: "nvfp4",
			SizeBytes: 2_950_000_000, Thinking: true,
		},
		{
			Name: lfm, Digest: digestOf(lfm),
			Format: "gguf", Family: "lfm2moe", Parameters: "8.3B", Quantization: "Q4_K_M",
			SizeBytes: 5_160_000_000,
		},
		{Name: "tiny:latest", Digest: "abc", Format: "gguf", SizeBytes: 999_950},
	}
}

func TestModelsGolden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := WriteModels(&buf, sampleModels()); err != nil {
		t.Fatalf("WriteModels() = %v", err)
	}
	golden.Assert(t, "models", buf.String())
}

func TestModelsWithoutModels(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := WriteModels(&buf, nil); err != nil {
		t.Fatalf("WriteModels() = %v", err)
	}
	if got, want := buf.String(), "NAME  DIGEST  FORMAT  FAMILY  PARAMS  QUANT  SIZE  THINKING\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestModelsWriteError(t *testing.T) {
	t.Parallel()
	if err := WriteModels(&failingWriter{}, sampleModels()); !errors.Is(err, errDiskFull) {
		t.Errorf("WriteModels() = %v, want %v", err, errDiskFull)
	}
}
