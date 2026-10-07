package report

import (
	"testing"

	"dyno/internal/domain"
)

func TestUnknownExternalProtocolDoesNotBecomeNative(t *testing.T) {
	t.Parallel()
	r := Row{Backend: domain.BackendOpenAI}
	if r.Protocol() != "unknown" || r.Native() {
		t.Fatalf("invented native protocol for external row: %s", r.Protocol())
	}
	if !(Row{}).Native() {
		t.Fatal("legacy native row lost historical default")
	}
}
