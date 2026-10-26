package importer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"ramseyer-finance/internal/db"
	"ramseyer-finance/internal/imports/plan"
	"strconv"
	"strings"
)

// labelMapSetting stores the last account chosen for each workbook label. The mapping
// belongs in settings rather than in the batch table because it is an operator preference
// that should survive replacing a year's transactions.
const labelMapSetting = "import.label_map"

// queryRower and execer are the narrow database surfaces the mapping and commit code need.
// Keeping them separate from *sql.DB lets the commit run every write inside one transaction
// while the preview can still read through the shared handle.
type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// LoadLabelMap reads the saved label choices. A missing setting is a normal first run. A
// malformed value is reported in the application log and ignored so a damaged preference
// cannot stop an operator from importing; the preview simply falls back to parser defaults.
func LoadLabelMap() (map[string]int64, error) {
	raw, err := db.GetSetting(labelMapSetting)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return map[string]int64{}, nil
	}

	mapping := make(map[string]int64)
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil {
		log.Printf("import: ignoring unreadable saved label map: %v", err)
		return map[string]int64{}, nil
	}
	return mapping, nil
}

// saveLabelMap upserts the mapping inside the commit transaction. Writing it with the same
// transaction as the transactions means a rollback cannot leave a saved choice pointing at
// an account that was never used.
func saveLabelMap(handle execer, mapping map[string]int64) error {
	payload, err := json.Marshal(mapping)
	if err != nil {
		return fmt.Errorf("encode label map: %w", err)
	}
	if _, err := handle.Exec(`
		INSERT INTO settings (key, value)
		VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, labelMapSetting, string(payload)); err != nil {
		return fmt.Errorf("save label map: %w", err)
	}
	return nil
}

// mergeLabelMap keeps choices for labels that are absent from the current workbook. The
// workbook gains and loses collection lines between years, and dropping the old choices
// would make every return of a label look new again.
func mergeLabelMap(saved, current map[string]int64) map[string]int64 {
	merged := make(map[string]int64, len(saved)+len(current))
	for key, categoryID := range saved {
		merged[key] = categoryID
	}
	for key, categoryID := range current {
		merged[key] = categoryID
	}
	return merged
}

// InitialMapping turns the saved choices and parser defaults into one account ID per label.
// A saved choice wins, but only while that account still exists and is active. Otherwise the
// parser default is used, and an unknown label is left at zero so the preview asks for it.
func InitialMapping(built *plan.Plan, categories []CategoryOption, saved map[string]int64) map[string]int64 {
	byName, byID := categoryIndexes(categories)
	mapping := make(map[string]int64, len(built.Labels))

	for _, label := range built.Labels {
		if categoryID, exists := saved[label.Key]; exists {
			if _, active := byID[categoryID]; active {
				mapping[label.Key] = categoryID
				continue
			}
		}
		if label.Category == "" {
			continue
		}
		if category, exists := byName[label.Category]; exists {
			mapping[label.Key] = category.ID
		}
	}
	return mapping
}

// MappingError reports labels that could not be resolved. It carries every valid selection
// so the handler can redraw the preview without discarding the operator's other choices.
type MappingError struct {
	UnresolvedKeys map[string]bool
	Selected       map[string]int64
}

func (e *MappingError) Error() string {
	count := len(e.UnresolvedKeys)
	if count == 1 {
		return "1 collection label still needs an account. Nothing was imported."
	}
	return fmt.Sprintf("%d collection labels still need an account. Nothing was imported.", count)
}

// resolveMapping validates the submitted form values against the active income accounts.
// A missing, non-numeric, or archived account is unresolved rather than silently replaced
// with a default, because the operator has already seen and confirmed the preview.
func resolveMapping(built *plan.Plan, categories []CategoryOption, submitted map[string]string) (map[string]int64, *MappingError) {
	_, byID := categoryIndexes(categories)
	mapping := make(map[string]int64, len(built.Labels))
	unresolved := make(map[string]bool)

	for _, label := range built.Labels {
		categoryID, err := strconv.ParseInt(strings.TrimSpace(submitted[label.Key]), 10, 64)
		if err != nil {
			unresolved[label.Key] = true
			continue
		}
		if _, active := byID[categoryID]; !active {
			unresolved[label.Key] = true
			continue
		}
		mapping[label.Key] = categoryID
	}

	if len(unresolved) > 0 {
		return mapping, &MappingError{UnresolvedKeys: unresolved, Selected: mapping}
	}
	return mapping, nil
}
