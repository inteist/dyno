package report

import (
	"dyno/internal/domain"
	"testing"
)

func TestOperationalMetadataDoesNotSplitComparableHistory(t *testing.T) {
	for _, base := range []Row{{ConfigJSON: domain.NativeConfigJSON}, {Backend: domain.BackendOpenAI, MeasurementProtocol: domain.ProtocolOpenAIChat, ContextPolicy: "server-managed", ConfigJSON: `{"usage":"required","omit_seed":false}`}} {
		other := base
		other.ConfigJSON = base.ConfigJSON[:len(base.ConfigJSON)-1] + `,"skip_model_discovery":true}`
		if base.PolicyKey() != other.PolicyKey() {
			t.Fatal("operational metadata split history", base.PolicyKey(), other.PolicyKey())
		}
	}
}
