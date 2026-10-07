package sqlite

import (
	"database/sql"
	"testing"

	"dyno/internal/domain"
	"dyno/internal/report"
)

func TestModelSizeUnknownAndKnownZeroRoundTrip(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	recorder := NewRecorder(db)
	session := testSession("sizes", day(1), "", "short", 16)
	models := []domain.Model{
		{Name: "unknown", Identity: "openai:session:unknown", MetadataAvailable: &domain.ModelMetadataAvailability{}},
		{Name: "known-zero", Identity: "openai:session:zero", MetadataAvailable: &domain.ModelMetadataAvailability{SizeBytes: true}},
	}
	exchanges := []domain.Exchange{
		exchange(session, models[0], domain.KindRun, domain.CacheMiss, 1, day(1), measurement(1)),
		exchange(session, models[1], domain.KindRun, domain.CacheMiss, 1, day(2), measurement(2)),
	}
	record(t, recorder, []domain.Session{session}, models, exchanges)
	for i, model := range models {
		var size sql.Null[int64]
		if err := db.QueryRowContext(t.Context(), "SELECT size_bytes FROM models WHERE model_key = ?", model.StorageKey()).Scan(&size); err != nil {
			t.Fatal(err)
		}
		if size.Valid != (i == 1) || size.V != 0 {
			t.Fatalf("bad raw size %s: %+v", model.Name, size)
		}
	}
	rows, err := NewSource(db).Rows(t.Context(), report.Filter{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	if rows[0].Model.HasSizeBytes() || !rows[1].Model.HasSizeBytes() {
		t.Fatal("model size availability was lost")
	}
	if rows[0].Model.HasThinking() || rows[1].Model.HasThinking() {
		t.Fatal("invented persisted thinking capability")
	}
}
