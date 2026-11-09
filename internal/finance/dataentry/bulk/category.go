package bulk

import (
	"strings"
)

// Category is the minimum of an account needed to resolve an uploaded label.
type Category struct {
	ID   int64
	Name string
}

// MatchCategory resolves a free text label from the sheet to one existing account. It
// never creates an account. An unknown or ambiguous label is reported back so the chart of
// accounts stays an explicit operator decision rather than an upload side effect.
//
// Exact match runs first because labels copied from the register should land immediately.
// The substring pass exists for the client's shorthand heads like "CHILDREN EXP", but it
// only resolves when exactly one account name contains the label. The comparison runs in
// that one direction on purpose: a head that merely contains a shorter account name, such
// as "SEMINAR & TRAINING" around "SEMINAR", is a distinct head and must not be folded into
// the shorter one. Two matches means ambiguity, and guessing would silently post to the
// wrong account.
func MatchCategory(categories []Category, label string) (int64, bool) {
	needle := strings.ToLower(strings.TrimSpace(label))
	if needle == "" {
		return 0, false
	}

	for _, category := range categories {
		if strings.ToLower(strings.TrimSpace(category.Name)) == needle {
			return category.ID, true
		}
	}

	var (
		matchID int64
		matches int
	)
	for _, category := range categories {
		name := strings.ToLower(strings.TrimSpace(category.Name))
		if strings.Contains(name, needle) {
			matchID = category.ID
			matches++
		}
	}
	if matches == 1 {
		return matchID, true
	}
	return 0, false
}
