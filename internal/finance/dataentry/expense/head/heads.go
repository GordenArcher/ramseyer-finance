package head

import "strings"

const (
	// ParentName is the chart account that collects expense heads the office sheet uses but
	// the chart of accounts does not carry. Keeping every imported head under one parent
	// stops a first upload from scattering new top-level lines through the chart.
	ParentName = "Other Expenditure"

	// ParentNote ties the imported heads to the general administration note so the statements
	// keep totalling them with the rest of that section.
	ParentNote = "20"
)

// aliases canonicalises the office shorthand in the expenditure book. Keys are compared
// after trimming and collapsing spaces, so "T & T" and "T&T" land on one head instead of
// growing two accounts for the same label.
var aliases = map[string]string{
	"REFESHMENT": "REFRESHMENT",
	"T&T":        "T&T",
	"T & T":      "T&T",
	"TIER2":      "TIER 2",
	"TIER 2":     "TIER 2",
	"OTHER":      "OTHERS",
}

// Normalize returns the spelling the app stores for one expense head. It only rewrites the
// variants the office workbook actually uses; every other head keeps the operator's wording
// so an unusual line still reads the way the office writes it.
func Normalize(label string) string {
	cleaned := strings.Join(strings.Fields(strings.TrimSpace(label)), " ")
	if cleaned == "" {
		return ""
	}
	if canonical, ok := aliases[strings.ToUpper(cleaned)]; ok {
		return canonical
	}
	return cleaned
}
