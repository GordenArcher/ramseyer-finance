package bulk

import (
	"testing"
	"time"
)

func TestSummaryStoreReturnsEachReportOnce(t *testing.T) {
	now := time.Date(2026, 1, 4, 12, 0, 0, 0, time.UTC)
	store := NewSummaryStore(time.Minute, 2, func() time.Time { return now })

	token := store.Put(ImportSummary{Sheet: "EXPENSES 2026", Imported: 3})
	if token == "" {
		t.Fatalf("Put returned an empty token")
	}

	summary, ok := store.Get(token)
	if !ok || summary.Imported != 3 {
		t.Fatalf("Get = %+v/%v, want the stored report", summary, ok)
	}
	if _, ok := store.Get(token); ok {
		t.Fatalf("report was returned twice")
	}
}

func TestSummaryStoreDropsExpiredReports(t *testing.T) {
	now := time.Date(2026, 1, 4, 12, 0, 0, 0, time.UTC)
	store := NewSummaryStore(time.Minute, 2, func() time.Time { return now })

	token := store.Put(ImportSummary{Sheet: "EXPENSES 2026"})
	now = now.Add(2 * time.Minute)

	if _, ok := store.Get(token); ok {
		t.Fatalf("expired report was still readable")
	}
}
