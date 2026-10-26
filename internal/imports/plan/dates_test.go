package plan

import "testing"

func TestParseSheetDate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		sheet     string
		year      int
		matched   bool
		wantDate  string
		wantError bool
	}{
		{name: "compact", sheet: "08MAR", year: 2026, matched: true, wantDate: "2026-03-08"},
		{name: "spaced", sheet: "29 MAR", year: 2026, matched: true, wantDate: "2026-03-29"},
		{name: "full month", sheet: "3APRIL", year: 2026, matched: true, wantDate: "2026-04-03"},
		{name: "trailing space", sheet: "4 OCT ", year: 2026, matched: true, wantDate: "2026-10-04"},
		{name: "not dated", sheet: "MAY", year: 2026, matched: false},
		{name: "invalid day", sheet: "31FEB", year: 2026, matched: true, wantError: true},
		{name: "unknown month", sheet: "5 FOO", year: 2026, matched: true, wantError: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			date, matched, err := ParseSheetDate(tc.sheet, tc.year)
			if matched != tc.matched {
				t.Fatalf("matched = %v, want %v", matched, tc.matched)
			}
			if tc.wantError {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantDate != "" && date.Format(DateLayout) != tc.wantDate {
				t.Fatalf("date = %s, want %s", date.Format(DateLayout), tc.wantDate)
			}
		})
	}
}
