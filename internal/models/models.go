package models

// Transaction represents a single financial transaction as it would be exposed through an
// API or serialised to JSON. Unlike the database row, this struct separates the category
// name into Category (the top-level parent) and Subcategory (the leaf category) for
// consumer convenience, avoiding the need for API clients to understand the two-level
// category hierarchy. The NoteRef field carries the accounting reference code from the
// categories table. All fields use JSON tags for consistent serialisation, and the ID and
// Amount fields use float64/int64 to match the database types directly.
type Transaction struct {
	ID          int64   `json:"id"`
	Date        string  `json:"date"`
	Type        string  `json:"type"`
	Category    string  `json:"category"`
	Subcategory string  `json:"subcategory"`
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
	NoteRef     string  `json:"note_ref"`
	CreatedAt   string  `json:"created_at"`
}

// Category represents a single row from the categories table. It includes the category's
// database ID, its type (income, expenditure, asset, or liability), its display name, an
// optional parent_id for hierarchical grouping (0 for top-level categories), and an
// optional note_ref code that maps this category to a specific line in the notes-to-the-
// accounts report. This struct mirrors the database schema exactly and is suitable for
// API responses that need to expose the full category tree.
type Category struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	ParentID int64  `json:"parent_id"`
	NoteRef  string `json:"note_ref"`
}

// Budget represents a single row from the budgets table, which stores annual budget
// amounts per top-level income or expenditure category. The Year field identifies the
// fiscal year, the CategoryID references the categories table (constrained to top-level
// income/expenditure categories by application logic), and Amount holds the budgeted
// figure. Budgets are used by the annual income statement to compute variance (actual
// minus budget) for each line item.
type Budget struct {
	ID         int64   `json:"id"`
	Year       int     `json:"year"`
	CategoryID int64   `json:"category_id"`
	Amount     float64 `json:"amount"`
}

// OpeningBalance represents a single row from the opening_balances table, which stores
// the carried-forward cash position for each liquid account at the start of a fiscal year.
// The Year identifies the fiscal year, AccountType is one of "bank", "cash", or "momo",
// and Amount is the starting balance for that account. Opening balances are added to
// in-year transaction movements to compute the current balance displayed on the dashboard
// and balance sheet.
type OpeningBalance struct {
	ID          int64   `json:"id"`
	Year        int     `json:"year"`
	AccountType string  `json:"account_type"`
	Amount      float64 `json:"amount"`
}

// CatOption represents a single selectable category in a dropdown or list UI component.
// It carries the database ID (used as the form submission value), the display name, and
// an Indent level for visual hierarchy. Categories with Indent=1 are subcategories and
// should be rendered with visual indentation (e.g., a "--" prefix or CSS padding) to
// distinguish them from top-level parent categories. This struct is used by both the
// data entry modals and the report filter dropdowns.
type CatOption struct {
	ID     int64
	Name   string
	Indent int
}

// BalanceLine represents a single row in a balance sheet section, pairing a display name
// with a monetary amount. It is used to populate the non-current assets, current assets,
// and liabilities sections of the balance sheet report. The absence of a prior-year amount
// field indicates this is a single-period view; comparative balance sheets use separate
// slices for current and prior periods rather than pairing them in this struct.
type BalanceLine struct {
	Name   string
	Amount float64
}

// LineRow represents a single line item in the annual income statement. It carries the
// note_ref for cross-referencing with the notes report, the category display name, the
// actual amount for the reporting year, the budgeted amount for variance calculation, and
// the computed variance (actual minus budget). The IsTotal and IsBold flags control
// template styling—summary rows at the bottom of each section use these flags to render
// with a border, bold text, and a distinct background colour.
type LineRow struct {
	Note     string
	Name     string
	Amount   float64
	Budget   float64
	Variance float64
	IsTotal  bool
	IsBold   bool
}

// MonthRow represents a single month's financial summary in the monthly breakdown report.
// It pairs the full month name (e.g., "January") with the aggregated income, expense, and
// computed surplus (income minus expense) for that calendar month. Months with no
// transactions appear with zero values across all three fields rather than being omitted,
// keeping the twelve-month report grid structurally complete.
type MonthRow struct {
	Month   string
	Income  float64
	Expense float64
	Surplus float64
}
