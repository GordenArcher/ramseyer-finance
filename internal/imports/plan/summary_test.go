package plan

import "testing"

func TestParseSummarySkipsWeeksWithWeeklySheets(t *testing.T) {
	t.Parallel()

	rows := [][]string{
		{"", "2026-01-04", "2026-01-11", "2026-01-18", "2026-01-25", "2026-02-01"},
		{"Sunday Service Offering", "100", "200", "300", "", ""},
		{"Second Offering", "10", "20", "30", "", ""},
		{"Tithe", "400", "500", "600", "", ""},
		{"TOTAL", "510", "720", "930", "", ""},
	}
	layout, ok := detectSummaryLayout(rows)
	if !ok {
		t.Fatal("summary layout was not detected")
	}

	weeklyDates := map[string]bool{"2026-01-11": true}
	parsed, report, warnings := parseSummarySheet("JAN TO JULY", rows, layout, weeklyDates, 2)

	if len(parsed) != 6 {
		t.Fatalf("parsed %d rows, want 6 (two weeks times the three collection rows)", len(parsed))
	}
	if report.Total != 510+930 {
		t.Fatalf("report total = %.2f, want %.2f", report.Total, 510.0+930.0)
	}
	if !report.Reconciles {
		t.Fatalf("summary did not reconcile: total %.2f expected %.2f", report.Total, report.Expected)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if parsed[0].Date.Format(DateLayout) != "2026-01-04" {
		t.Fatalf("first row date = %s, want 2026-01-04", parsed[0].Date.Format(DateLayout))
	}
}
