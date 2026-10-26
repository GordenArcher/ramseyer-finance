package plan

import (
	"fmt"
	"time"
)

// DateLayout is the storage and display format the rest of the application uses for
// transaction dates. Keeping it here means the importer and the register cannot drift.
const DateLayout = "2006-01-02"

// CategorySource records how a row's category was chosen. The preview uses this to show
// confident matches separately from heuristic guesses that the operator should check.
type CategorySource string

const (
	// CategoryStandard means the workbook label maps directly to an existing account.
	CategoryStandard CategorySource = "standard"
	// CategoryHeuristic means the label names a themed or project collection and the
	// account below is an educated default rather than a direct match.
	CategoryHeuristic CategorySource = "heuristic"
	// CategoryUndecided means the importer has no safe default and the operator must
	// choose an account before the batch can be saved.
	CategoryUndecided CategorySource = "undecided"
)

// Row is one collection amount that will become one income transaction. A weekly sheet
// row with separate first and second service amounts produces two Rows, because the
// register keeps one amount per transaction and the service belongs in the description.
type Row struct {
	Date       time.Time
	Label      string
	Key        string
	Category   string
	Source     CategorySource
	Amount     float64
	Service    string
	Sheet      string
	Line       int
	SheetOrder int
}

// Description is the register text for the row. The category already carries the account
// name, so the description keeps the source label and the service that produced the amount.
func (r Row) Description() string {
	if r.Service == "" {
		return r.Label
	}
	return fmt.Sprintf("%s (%s service)", r.Label, r.Service)
}

// Label summarises every row that shares a workbook label. The preview decides mappings
// per label rather than per row so one choice covers all occurrences of the same line.
type Label struct {
	Key      string
	Display  string
	Rows     int
	Total    float64
	Category string
	Source   CategorySource
}

// SheetKind describes how the importer treated a worksheet. The report uses this to show
// what was imported and, more importantly, what was deliberately left out.
type SheetKind string

const (
	KindWeekly         SheetKind = "weekly"
	KindSummary        SheetKind = "summary"
	KindPledgeList     SheetKind = "pledge"
	KindFundLedger     SheetKind = "fund"
	KindUndatedSummary SheetKind = "undated-summary"
	KindUnrecognised   SheetKind = "unrecognised"
)

// SheetReport is the per-sheet outcome shown above the preview table. Expected and Total
// come from the workbook's own total row so a parsing mistake is visible before saving.
type SheetReport struct {
	Name       string
	Kind       SheetKind
	Rows       int
	Total      float64
	Expected   float64
	Reconciles bool
	Note       string
}

// Plan is a complete, unsaved reading of one workbook. It contains no database IDs: the
// operator reviews and maps labels first, then the importer resolves the final accounts.
type Plan struct {
	FileName string
	FileHash string
	Year     int
	Rows     []Row
	Labels   []Label
	Sheets   []SheetReport
	Warnings []string
}

// Total is the amount the plan would add across all parsed rows.
func (p *Plan) Total() float64 {
	var total float64
	for _, row := range p.Rows {
		total += row.Amount
	}
	return total
}

// UndecidedLabels returns the labels that still need an account. The handler uses this to
// block the save button until every amount has a home.
func (p *Plan) UndecidedLabels() []Label {
	labels := make([]Label, 0, len(p.Labels))
	for _, label := range p.Labels {
		if label.Source == CategoryUndecided {
			labels = append(labels, label)
		}
	}
	return labels
}
