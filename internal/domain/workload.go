package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Workload holds every request setting that changes the speed.
type Workload struct {
	PromptName  string
	PromptHash  string
	NumPredict  int
	NumCtx      int
	Temperature float64
	Seed        int
}

// Hash returns a short, stable ID. The prompt name is not part of it, so a
// renamed prompt file keeps its history.
func (w Workload) Hash() string {
	sum := sha256.Sum256(fmt.Appendf(nil, "v1|%s|%d|%d|%g|%d",
		w.PromptHash, w.NumPredict, w.NumCtx, w.Temperature, w.Seed))
	return hex.EncodeToString(sum[:6])
}
