package plan

import (
	"testing"
	"time"
)

func TestParseAmount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value string
		want  float64
		ok    bool
	}{
		{value: "1,234.50", want: 1234.5, ok: true},
		{value: "GH₵ 50", want: 50, ok: true},
		{value: "(100)", want: -100, ok: true},
		{value: "", ok: false},
		{value: "not money", ok: false},
	}

	for _, tc := range cases {
		got, ok := parseAmount(tc.value)
		if ok != tc.ok {
			t.Errorf("parseAmount(%q) ok = %v, want %v", tc.value, ok, tc.ok)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("parseAmount(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestParseWeeklySheetSplitsServices(t *testing.T) {
	t.Parallel()

	rows := [][]string{
		{"", "", "", ""},
		{"", "1ST", "2ND", "TOTAL"},
		{"Sunday Service Offering", "1000", "500", "1500"},
		{"Second Service Offering", "700", "", "700"},
		{"Tithe", "2000", "", "2000"},
		{"0", "0", "", "0"},
		{"TOTAL", "3700", "500", "4200"},
		{"CASH", "4200", "", ""},
	}
	layout, ok := detectWeeklyLayout(rows)
	if !ok {
		t.Fatal("weekly layout was not detected")
	}

	date := time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC)
	parsed, report, warnings := parseWeeklySheet("08MAR", date, rows, layout, 1)

	if len(parsed) != 4 {
		t.Fatalf("parsed %d rows, want 4", len(parsed))
	}
	if parsed[0].Service != "1st" || parsed[0].Amount != 1000 {
		t.Fatalf("first row = %q/%.2f, want 1st/1000", parsed[0].Service, parsed[0].Amount)
	}
	if parsed[1].Service != "2nd" || parsed[1].Amount != 500 {
		t.Fatalf("second row = %q/%.2f, want 2nd/500", parsed[1].Service, parsed[1].Amount)
	}
	if parsed[2].Service != "2nd" || parsed[2].Amount != 700 {
		t.Fatalf("single second offering = %q/%.2f, want 2nd/700", parsed[2].Service, parsed[2].Amount)
	}
	if parsed[3].Service != "" || parsed[3].Amount != 2000 {
		t.Fatalf("tithe row = %q/%.2f, want no service/2000", parsed[3].Service, parsed[3].Amount)
	}
	if !report.Reconciles {
		t.Fatalf("report did not reconcile: total %.2f expected %.2f", report.Total, report.Expected)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
}

func TestParseWeeklySheetFlagsTotalMismatch(t *testing.T) {
	t.Parallel()

	rows := [][]string{
		{"", "1ST", "2ND", "TOTAL"},
		{"Tithe", "90", "", "90"},
		{"TOTAL", "90", "", "100"},
	}
	layout, ok := detectWeeklyLayout(rows)
	if !ok {
		t.Fatal("weekly layout was not detected")
	}

	_, report, warnings := parseWeeklySheet("BAD", time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC), rows, layout, 1)
	if report.Reconciles {
		t.Fatal("expected the total mismatch to be reported")
	}
	if len(warnings) == 0 {
		t.Fatal("expected a mismatch warning")
	}
}
