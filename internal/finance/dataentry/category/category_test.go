package category

import "testing"

func TestMatchCategoryNeverGuessesBetweenTwoAccounts(t *testing.T) {
	categories := []Category{
		{ID: 1, Name: "Printing & Stationery"},
		{ID: 2, Name: "Stationery Supplies"},
		{ID: 3, Name: "Utilities-Manse"},
	}

	if id, ok := MatchCategory(categories, "printing & stationery"); !ok || id != 1 {
		t.Fatalf("exact match = %d/%v, want 1/true", id, ok)
	}

	// "stationery" sits inside two different account names, so resolving it would be a
	// guess and a wrong posting the operator never sees.
	if id, ok := MatchCategory(categories, "stationery"); ok {
		t.Fatalf("ambiguous label resolved to %d", id)
	}

	// A single substring hit still resolves, which is how the office shorthand heads
	// like "MANSE" reach the full account name.
	if id, ok := MatchCategory(categories, "manse"); !ok || id != 3 {
		t.Fatalf("unique substring match = %d/%v, want 3/true", id, ok)
	}

	if _, ok := MatchCategory(categories, ""); ok {
		t.Fatalf("empty label resolved to an account")
	}
}

func TestMatchCategoryDoesNotFoldALongerHeadIntoAShorterAccount(t *testing.T) {
	categories := []Category{
		{ID: 1, Name: "Seminar"},
		{ID: 2, Name: "Kutunse"},
	}

	// The office book keeps SEMINAR and SEMINAR & TRAINING as separate heads. A label that
	// merely contains a shorter account name must become its own head, not be folded in.
	if id, ok := MatchCategory(categories, "Seminar & Training"); ok {
		t.Fatalf("longer head resolved to %d, want a separate head", id)
	}
	if id, ok := MatchCategory(categories, "Kutunse-To GM"); ok {
		t.Fatalf("longer head resolved to %d, want a separate head", id)
	}
}
