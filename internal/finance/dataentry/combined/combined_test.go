package combined

import "testing"

func TestNormalizeTransactionType(t *testing.T) {
	cases := map[string]string{
		"income":      "income",
		"Receipts":    "income",
		"expense":     "expenditure",
		"EXPENDITURE": "expenditure",
		"assets":      "asset",
		"Liability":   "liability",
	}
	for raw, want := range cases {
		got, ok := NormalizeTransactionType(raw)
		if !ok || got != want {
			t.Fatalf("NormalizeTransactionType(%q) = %q, %v; want %q, true", raw, got, ok, want)
		}
	}

	if _, ok := NormalizeTransactionType("donation"); ok {
		t.Fatalf("donation is not a transaction type")
	}
}
