package finance

import (
	"fmt"
	"ramseyer-finance/internal/db"
)

func loadReportingWarnings(year int) ([]string, error) {
	// I turn missing note mappings into explicit warnings because the more dangerous failure mode
	// in financial reporting is a clean-looking statement that silently omitted real activity.
	_, endDate := yearBounds(year)
	priorStartDate, _ := yearBounds(year - 1)

	rows, err := db.DB.Query(`
		SELECT c.name, c.type, COALESCE(SUM(t.amount), 0)
		FROM categories c
		LEFT JOIN financial_postings t
			ON t.category_id = c.id
			AND t.account_type = c.type
			AND t.date >= ?
			AND t.date < ?
		WHERE c.type IN ('income', 'expenditure')
			AND c.parent_id = 0
			AND COALESCE(c.note_ref, '') = ''
		GROUP BY c.id, c.name, c.type
		HAVING COALESCE(SUM(t.amount), 0) <> 0
		ORDER BY c.type, c.name
	`, priorStartDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("query report warnings: %w", err)
	}
	defer rows.Close()

	var warnings []string
	for rows.Next() {
		var (
			name         string
			categoryType string
			amount       float64
		)
		if err := rows.Scan(&name, &categoryType, &amount); err != nil {
			return nil, fmt.Errorf("scan report warning: %w", err)
		}
		warnings = append(
			warnings,
			fmt.Sprintf("%s category %q has transactions but no note reference mapping. Notes may exclude part of this activity.", categoryType, name),
		)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate report warnings: %w", err)
	}

	return warnings, nil
}
