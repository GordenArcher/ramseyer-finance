package importer

import (
	"ramseyer-finance/internal/imports/plan"
	"testing"
	"time"
)

func TestPreviewStoreExpiresEntries(t *testing.T) {
	now := time.Date(2026, time.January, 4, 9, 0, 0, 0, time.UTC)
	store := newPreviewStore(time.Minute, 4, func() time.Time { return now })

	token, err := store.Put(&plan.Plan{FileName: "first.xlsx"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := store.Get(token); !ok {
		t.Fatal("entry should exist before the TTL")
	}

	now = now.Add(time.Minute)
	if _, ok := store.Get(token); ok {
		t.Fatal("entry should expire at the TTL")
	}
}

func TestPreviewStoreEvictsOldestWhenFull(t *testing.T) {
	now := time.Date(2026, time.January, 4, 9, 0, 0, 0, time.UTC)
	store := newPreviewStore(time.Hour, 2, func() time.Time { return now })

	first, err := store.Put(&plan.Plan{FileName: "first.xlsx"})
	if err != nil {
		t.Fatalf("Put first: %v", err)
	}
	now = now.Add(time.Second)
	second, err := store.Put(&plan.Plan{FileName: "second.xlsx"})
	if err != nil {
		t.Fatalf("Put second: %v", err)
	}
	now = now.Add(time.Second)
	third, err := store.Put(&plan.Plan{FileName: "third.xlsx"})
	if err != nil {
		t.Fatalf("Put third: %v", err)
	}

	if _, ok := store.Get(first); ok {
		t.Fatal("oldest entry should have been evicted")
	}
	if stored, ok := store.Get(second); !ok || stored.FileName != "second.xlsx" {
		t.Fatalf("second entry = %v/%v, want second.xlsx", stored, ok)
	}
	if stored, ok := store.Get(third); !ok || stored.FileName != "third.xlsx" {
		t.Fatalf("third entry = %v/%v, want third.xlsx", stored, ok)
	}
}

func TestPreviewStoreDeleteRemovesEntry(t *testing.T) {
	store := newPreviewStore(time.Hour, 2, time.Now)

	token, err := store.Put(&plan.Plan{FileName: "first.xlsx"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	store.Delete(token)

	if _, ok := store.Get(token); ok {
		t.Fatal("deleted entry should not be returned")
	}
}
