package head

import "testing"

func TestNormalizeCanonicalisesTheOfficeShorthand(t *testing.T) {
	cases := map[string]string{
		"REFESHMENT":     "REFRESHMENT",
		"  refreshment ": "refreshment",
		"T & T":          "T&T",
		"T&T":            "T&T",
		"t & t":          "T&T",
		"TIER2":          "TIER 2",
		"TIER 2":         "TIER 2",
		"  tier   2  ":   "TIER 2",
		"OTHER":          "OTHERS",
		"OTHERS":         "OTHERS",
		"SEND OFF":       "SEND OFF",
		"":               "",
	}
	for input, want := range cases {
		if got := Normalize(input); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", input, got, want)
		}
	}
}
