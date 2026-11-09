package value

import "testing"

func TestParseDateAcceptsSerialsAndText(t *testing.T) {
	cases := map[string]string{
		"46026":               "2026-01-04", // Excel serial, the form a real date cell takes
		"2026-01-04":          "2026-01-04",
		"04/01/2026":          "2026-01-04", // day first, which is what the office workbook uses
		"4/1/2026":            "2026-01-04",
		"04-01-2026":          "2026-01-04",
		"4-Jan-2026":          "2026-01-04",
		"Jan 4, 2026":         "2026-01-04",
		"2026-01-04 00:00:00": "2026-01-04",
	}
	for raw, want := range cases {
		got, ok := ParseDate(raw)
		if !ok {
			t.Fatalf("ParseDate(%q) rejected a real date", raw)
		}
		if got != want {
			t.Fatalf("ParseDate(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestParseDateRejectsJunk(t *testing.T) {
	for _, raw := range []string{"", "closing bal.", "not a date", "0"} {
		if got, ok := ParseDate(raw); ok {
			t.Fatalf("ParseDate(%q) accepted a non date as %q", raw, got)
		}
	}
}

func TestParseAmountStripsCurrencyDecoration(t *testing.T) {
	cases := map[string]float64{
		"1400":       1400,
		"1,400.50":   1400.50,
		"GH¢ 960.75": 960.75,
		"GHS 2,300":  2300,
		"(400)":      -400,
	}
	for raw, want := range cases {
		got, err := ParseAmount(raw)
		if err != nil {
			t.Fatalf("ParseAmount(%q) failed: %v", raw, err)
		}
		if got != want {
			t.Fatalf("ParseAmount(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestParseAmountRejectsFormulas(t *testing.T) {
	if _, err := ParseAmount("=SUM(C3:C4)"); err == nil {
		t.Fatalf("ParseAmount accepted a formula")
	}
}
