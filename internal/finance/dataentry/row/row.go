package row

import "strings"

// Cell returns one column from a row without making every caller repeat the bounds check.
func Cell(cells []string, index int) string {
	if index < 0 || index >= len(cells) {
		return ""
	}
	return cells[index]
}

// IsBlank reports whether every cell in the row is empty or whitespace.
func IsBlank(cells []string) bool {
	for _, value := range cells {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

// IsSummary recognises the subtotal and opening balance rows that appear between real
// entries. They carry a formula or a running total in the amount column, so they must never
// become postings.
func IsSummary(cells []string) bool {
	for _, value := range cells {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "=") ||
			strings.HasPrefix(trimmed, "closing bal") ||
			strings.HasPrefix(trimmed, "opening bal") {
			return true
		}
	}
	return false
}
