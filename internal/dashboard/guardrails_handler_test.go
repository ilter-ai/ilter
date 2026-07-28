package dashboard

import (
	"encoding/csv"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db/dbtest"
)

// TestHandleGuardrailExport_CSVEscaping is a regression test: details/model
// values containing commas, quotes, or newlines must not corrupt the CSV
// structure. The handler used to build rows via fmt.Sprintf with no escaping.
func TestHandleGuardrailExport_CSVEscaping(t *testing.T) {
	store := dbtest.New(t)

	if err := store.InsertGuardrailEvent("key-1", "pii", "blocked", "gpt-4o", "openai",
		`matched "SSN", also comma, and a
newline`); err != nil {
		t.Fatalf("InsertGuardrailEvent: %v", err)
	}

	h := NewGuardrailsHandler(nil, store, &config.Config{}, nil)

	req := httptest.NewRequest("GET", "/guardrails/export?format=csv", nil)
	rr := httptest.NewRecorder()
	h.HandleGuardrailExport(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	rows, err := csv.NewReader(strings.NewReader(rr.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("response body is not valid CSV: %v\nbody:\n%s", err, rr.Body.String())
	}
	if len(rows) != 2 {
		t.Fatalf("expected header + 1 data row, got %d rows: %+v", len(rows), rows)
	}
	details := rows[1][7] // details is the 8th column
	if !strings.Contains(details, `matched "SSN", also comma, and a`) {
		t.Errorf("details field corrupted, got: %q", details)
	}
}
