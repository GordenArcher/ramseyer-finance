package importer

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"ramseyer-finance/internal/db"
	"ramseyer-finance/internal/imports/plan"
)

// ErrUnreadableWorkbook marks a failure in the uploaded file itself, as opposed to a
// database or server failure. The handler can show the parser's explanation to the operator
// without turning every import error into an internal server error.
var ErrUnreadableWorkbook = errors.New("workbook could not be read")

// BatchSummary describes the existing import for a year. The confirmation screen shows it
// before the operator agrees to replace it, so a re-upload is never a surprise deletion.
type BatchSummary struct {
	ID         int64
	FileName   string
	RowCount   int
	Total      float64
	ImportedAt string
}

// LabelView is one workbook label with the account that would be used for it. CategoryID
// stays zero when no safe account exists, which is the condition the commit refuses.
type LabelView struct {
	Key           string
	Display       string
	Rows          int
	Total         float64
	Source        plan.CategorySource
	CategoryID    int64
	CategoryLabel string
}

// Preview is everything the import screen needs to render after reading a workbook. The
// parsed plan is not edited after this point; the operator's changes arrive with the commit
// form and are validated against the live chart of accounts there.
type Preview struct {
	Token         string
	Plan          *plan.Plan
	Categories    []CategoryOption
	Labels        []LabelView
	ExistingBatch *BatchSummary
	Warnings      []string
	Unresolved    map[string]bool
	Message       string
	MessageTone   string
}

// PreviewWorkbook reads an upload, stores the parsed plan, and builds the review screen.
// The hash identifies the exact file the operator confirmed, so a preview can be explained
// later without keeping the workbook itself.
func PreviewWorkbook(store *PreviewStore, fileName string, data []byte, yearOverride int) (*Preview, error) {
	sum := sha256.Sum256(data)
	built, err := plan.Build(data, plan.Options{
		FileName: filepath.Base(fileName),
		FileHash: hex.EncodeToString(sum[:]),
		Year:     yearOverride,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadableWorkbook, err)
	}

	categories, err := ActiveIncomeCategories(db.DB)
	if err != nil {
		return nil, err
	}
	saved, err := LoadLabelMap()
	if err != nil {
		return nil, err
	}
	existing, err := FindBatchForYear(built.Year)
	if err != nil {
		return nil, err
	}

	token, err := store.Put(built)
	if err != nil {
		return nil, err
	}

	preview := newPreview(token, built, categories, InitialMapping(built, categories, saved), existing)
	return &preview, nil
}

// PreviewSelections rebuilds the screen after a commit validation error. It uses the
// operator's submitted choices as they are, without falling back to parser defaults, so an
// empty or rejected field stays visible instead of being quietly replaced.
func PreviewSelections(token string, built *plan.Plan, selected map[string]int64) (*Preview, error) {
	categories, err := ActiveIncomeCategories(db.DB)
	if err != nil {
		return nil, err
	}
	existing, err := FindBatchForYear(built.Year)
	if err != nil {
		return nil, err
	}
	preview := newPreview(token, built, categories, selected, existing)
	return &preview, nil
}

func newPreview(token string, built *plan.Plan, categories []CategoryOption, mapping map[string]int64, existing *BatchSummary) Preview {
	_, byID := categoryIndexes(categories)

	labels := make([]LabelView, 0, len(built.Labels))
	unresolved := make(map[string]bool)
	for _, label := range built.Labels {
		view := LabelView{
			Key:     label.Key,
			Display: label.Display,
			Rows:    label.Rows,
			Total:   label.Total,
			Source:  label.Source,
		}
		if category, exists := byID[mapping[label.Key]]; exists {
			view.CategoryID = category.ID
			view.CategoryLabel = category.Label
		} else {
			unresolved[label.Key] = true
		}
		labels = append(labels, view)
	}

	warnings := append([]string{}, built.Warnings...)
	if len(unresolved) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d collection labels still need an account before the import can be saved.", len(unresolved)))
	}

	return Preview{
		Token:         token,
		Plan:          built,
		Categories:    categories,
		Labels:        labels,
		ExistingBatch: existing,
		Warnings:      warnings,
		Unresolved:    unresolved,
	}
}

// FindBatchForYear returns the batch that a new import would replace. No row is a normal
// first import for that year.
func FindBatchForYear(year int) (*BatchSummary, error) {
	return findBatchForYear(db.DB, year)
}

func findBatchForYear(handle queryRower, year int) (*BatchSummary, error) {
	var batch BatchSummary
	err := handle.QueryRow(`
		SELECT id, file_name, row_count, total_amount, COALESCE(imported_at, '')
		FROM import_batches
		WHERE year = ?
	`, year).Scan(&batch.ID, &batch.FileName, &batch.RowCount, &batch.Total, &batch.ImportedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup import batch for %d: %w", year, err)
	}
	return &batch, nil
}
