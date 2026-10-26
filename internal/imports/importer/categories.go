package importer

import (
	"database/sql"
	"fmt"
)

// CategoryOption is one active income account the operator can assign to a workbook label.
// Label carries the same "Parent / Name" wording as the rest of the app so the preview
// dropdown does not introduce a second naming convention.
type CategoryOption struct {
	ID      int64
	Name    string
	Label   string
	NoteRef string
}

// ActiveIncomeCategories loads the accounts an imported collection may post to. Archived
// accounts are excluded because the register would still show them in history but no new
// entry should be able to choose them.
func ActiveIncomeCategories(handle *sql.DB) ([]CategoryOption, error) {
	rows, err := handle.Query(`
		SELECT c.id, c.name, COALESCE(parent.name, ''), COALESCE(c.note_ref, '')
		FROM categories c
		LEFT JOIN categories parent ON parent.id = c.parent_id
		WHERE c.type = 'income' AND COALESCE(c.is_active, 1) = 1
		ORDER BY
			CASE WHEN c.parent_id = 0 THEN c.id ELSE parent.id END,
			c.parent_id,
			c.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query income categories: %w", err)
	}
	defer rows.Close()

	options := make([]CategoryOption, 0)
	for rows.Next() {
		var (
			option     CategoryOption
			parentName string
		)
		if err := rows.Scan(&option.ID, &option.Name, &parentName, &option.NoteRef); err != nil {
			return nil, fmt.Errorf("scan income category: %w", err)
		}
		if parentName != "" {
			option.Label = parentName + " / " + option.Name
		} else {
			option.Label = option.Name
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate income categories: %w", err)
	}
	return options, nil
}

// categoryIndexes builds the two lookups the mapping needs. The name lookup lets a parser
// default such as "Tithes" find the account's current ID, while the ID lookup validates a
// saved or submitted choice without trusting the client.
func categoryIndexes(categories []CategoryOption) (map[string]CategoryOption, map[int64]CategoryOption) {
	byName := make(map[string]CategoryOption, len(categories))
	byID := make(map[int64]CategoryOption, len(categories))
	for _, category := range categories {
		byID[category.ID] = category
		if _, exists := byName[category.Name]; !exists {
			byName[category.Name] = category
		}
	}
	return byName, byID
}
