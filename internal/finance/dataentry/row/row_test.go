package row

import "testing"

func TestIsSummaryRecognisesTotalsAndBalances(t *testing.T) {
	if !IsSummary([]string{"", "", "=SUM(C3:C4)", ""}) {
		t.Fatalf("formula subtotal was not recognised")
	}
	if !IsSummary([]string{"closing bal.", "", "", ""}) {
		t.Fatalf("closing balance row was not recognised")
	}
	if IsSummary([]string{"2026-01-04", "PLAN OF WORK", "456", "Printing & Stationery"}) {
		t.Fatalf("a real entry was treated as a summary")
	}
}
