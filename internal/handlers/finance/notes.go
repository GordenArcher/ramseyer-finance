package finance

import (
	"database/sql"
	"fmt"
	"net/http"
	"ramseyer-finance/internal/db"
)

// NotesData carries all the template variables for the notes-to-the-accounts report page.
// It includes the current and prior year labels, the list of available years for the
// picker dropdown, any reporting warnings (e.g., missing opening balances or orphaned
// transactions), and the ordered list of note sections. Each section corresponds to a
// note_ref group from the categories table, and includes a title, category type, the line
// items with comparative amounts, and section totals for both the current and prior year.
type NotesData struct {
	Active    string
	Year      string
	PriorYear string
	Years     []int
	Warnings  []string
	Notes     []NoteSection
}

// NoteSection represents a single numbered note in the report, grouping together all
// categories that share the same note_ref code. For example, note_ref "1" groups all
// offering-related income categories. The section carries a display number, a title
// (resolved from the noteTitle lookup table), the category type for colour context, the
// individual line items with comparative amounts, and the aggregated totals for both the
// current and prior year periods.
type NoteSection struct {
	Number       string
	Title        string
	CategoryType string
	Lines        []NoteLine
	Total        float64
	PriorTotal   float64
}

// NoteLine represents a single category row within a note section. It pairs a display name
// with a current-year amount and a prior-year amount, enabling side-by-side comparison in
// the report template. Lines with zero amounts in both periods are omitted from the output
// to keep the notes concise and focused on active categories.
type NoteLine struct {
	Name        string
	Amount      float64
	PriorAmount float64
}

// NotesPage serves the notes-to-the-accounts report for a given year. It parses the "year"
// query parameter, builds the full notes dataset including comparative prior-year figures
// and any reporting warnings, and renders the template. The notes report provides a
// structured breakdown of income and expenditure by accounting note reference, supplementing
// the income statement with the detailed category groupings required for financial reporting.
func NotesPage(w http.ResponseWriter, r *http.Request) {
	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	data, err := buildNotesData(year)
	if err != nil {
		serverError(w, err)
		return
	}
	RenderTemplate(w, "notes", data)
}

// buildNotesData assembles the complete NotesData struct for a given report year. It loads
// the available years for the picker, fetches any reporting warnings (such as unclassified
// transactions or missing opening balances), and then queries all income and expenditure
// categories that have a non-empty note_ref, computing both current-year and prior-year
// totals in a single pass. The categories are grouped into sections by note_ref, with top-
// level categories contributing their own direct activity before their children are appended—
// this reflects the reality that users can post transactions to either parent or child
// categories. Sections are ordered by type (income first, then expenditure) and then by
// note_ref number, producing a conventional financial report layout.
func buildNotesData(year int) (NotesData, error) {
	// I build notes from category mappings instead of hard-coded rows so the note structure stays
	// tied to the same category tree the user is actually posting into.
	years, err := reportYears(year)
	if err != nil {
		return NotesData{}, fmt.Errorf("load report years: %w", err)
	}

	data := NotesData{
		Active:    "notes",
		Year:      fmt.Sprintf("%d", year),
		PriorYear: fmt.Sprintf("%d", year-1),
		Years:     years,
	}
	// Reporting warnings highlight data quality issues (e.g., transactions without a
	// valid category, or categories with unlinked note_ref values) that could affect
	// the accuracy of the notes. These are displayed at the top of the report so the
	// user can address them before relying on the figures.
	data.Warnings, err = loadReportingWarnings(year)
	if err != nil {
		return NotesData{}, fmt.Errorf("load note warnings: %w", err)
	}

	// I query both current and prior periods in one pass so each note line is assembled once with
	// both comparative values attached. Income and expenditure are period flows, while asset and
	// liability notes are year-end balances, so the CASE expressions deliberately use different
	// date boundaries for those two accounting behaviours.
	// The query uses conditional aggregation (SUM with CASE WHEN) to compute current-year
	// and prior-year totals in the same row, avoiding a separate query or JOIN for the
	// prior period. The date range for the LEFT JOIN covers the full two-year span
	// (priorStartDate to endDate) so that all relevant transactions are available for
	// the CASE expressions to filter.
	startDate, endDate := yearBounds(year)
	priorStartDate, priorEndDate := yearBounds(year - 1)
	rows, err := db.DB.Query(`
		SELECT
			c.id,
			c.type,
			c.name,
			c.parent_id,
			c.note_ref,
			COALESCE(SUM(CASE
				WHEN c.type IN ('income', 'expenditure') AND t.date >= ? AND t.date < ? THEN t.amount
				WHEN c.type IN ('asset', 'liability') AND t.date < ? THEN t.amount
				ELSE 0
			END), 0),
			COALESCE(SUM(CASE
				WHEN c.type IN ('income', 'expenditure') AND t.date >= ? AND t.date < ? THEN t.amount
				WHEN c.type IN ('asset', 'liability') AND t.date < ? THEN t.amount
				ELSE 0
			END), 0)
		FROM categories c
		LEFT JOIN transactions t
			ON t.category_id = c.id
			AND t.type = c.type
			AND t.date >= ?
			AND t.date < ?
		WHERE c.type IN ('income', 'expenditure', 'asset', 'liability') AND c.note_ref <> ''
		GROUP BY c.id, c.type, c.name, c.parent_id, c.note_ref
		ORDER BY
			CASE c.type WHEN 'income' THEN 0 WHEN 'expenditure' THEN 1 WHEN 'asset' THEN 2 ELSE 3 END,
			CAST(c.note_ref AS INTEGER),
			c.parent_id,
			c.id
	`, startDate, endDate, endDate, priorStartDate, priorEndDate, priorEndDate, priorStartDate, endDate)
	if err != nil {
		return NotesData{}, fmt.Errorf("query note categories: %w", err)
	}
	defer rows.Close()

	// categoryTotal holds the query result for a single category row, including both the
	// current-year and prior-year aggregated amounts computed by the CASE expressions.
	type categoryTotal struct {
		ID          int64
		Type        string
		Name        string
		ParentID    int64
		NoteRef     string
		Amount      float64
		PriorAmount float64
	}

	// Collect all category rows and build a parent→children lookup map for assembling
	// the hierarchical sections. Children are grouped by their parent_id so that when
	// processing a top-level category, its subcategories can be appended immediately
	// after it in the section's line list.
	var categories []categoryTotal
	childrenByParent := map[int64][]categoryTotal{}
	for rows.Next() {
		var category categoryTotal
		if err := rows.Scan(&category.ID, &category.Type, &category.Name, &category.ParentID, &category.NoteRef, &category.Amount, &category.PriorAmount); err != nil {
			return NotesData{}, fmt.Errorf("scan note category: %w", err)
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		return NotesData{}, fmt.Errorf("iterate note categories: %w", err)
	}
	currentPositions, err := loadCategoryPositions(year)
	if err != nil {
		return NotesData{}, fmt.Errorf("load current note account positions: %w", err)
	}
	priorPositions, err := loadCategoryPositions(year - 1)
	if err != nil {
		return NotesData{}, fmt.Errorf("load prior note account positions: %w", err)
	}
	for index := range categories {
		if categories[index].Type == "asset" || categories[index].Type == "liability" {
			categories[index].Amount = currentPositions[categories[index].ID]
			categories[index].PriorAmount = priorPositions[categories[index].ID]
		}
		if categories[index].ParentID > 0 {
			childrenByParent[categories[index].ParentID] = append(childrenByParent[categories[index].ParentID], categories[index])
		}
	}

	// sectionOrder preserves the insertion order of note sections so they appear in the
	// report in the same sequence as the database query's ORDER BY clause. A map alone
	// would lose ordering since Go map iteration is randomised.
	sectionOrder := []string{}
	sections := map[string]*NoteSection{}

	// I let a parent category contribute its own activity before I append children because users
	// are allowed to post directly to top-level categories. Skipping the parent would understate notes.
	// Process each category from the ordered query results. Top-level categories (parent_id=0)
	// with a note_ref define a new section or extend an existing one. Each top-level category
	// contributes its own direct transactions as a line, then all of its child categories are
	// appended as sub-lines. The section total aggregates both the parent and all children,
	// giving a complete view of the note group's financial activity.
	for _, category := range categories {
		if category.ParentID != 0 || category.NoteRef == "" {
			continue
		}

		// Use a composite key of type + note_ref to distinguish between income and
		// expenditure sections that might share the same note_ref number. In practice
		// note_refs are unique across types, but this is a defensive measure.
		sectionKey := category.NoteRef
		// Note 21 contains both the depreciation/amortization expense disclosure and the
		// non-current-asset schedule. They share a workbook note number but must remain two
		// separate tables or their fundamentally different amounts would be added together.
		if category.NoteRef == "21" {
			sectionKey += ":" + category.Type
		}
		section, ok := sections[sectionKey]
		if !ok {
			section = &NoteSection{
				Number:       category.NoteRef,
				Title:        noteTitle(category.NoteRef, category.Name),
				CategoryType: category.Type,
			}
			sections[sectionKey] = section
			sectionOrder = append(sectionOrder, sectionKey)
		}

		// Only add the parent category as a visible line if it has non-zero activity
		// in at least one of the two periods. Categories with zero in both periods
		// are hidden to keep the notes concise—the section still exists because it
		// may have active children, but the parent line is omitted.
		amountSign := 1.0
		if category.Type == "expenditure" && category.NoteRef == "5" {
			// Harvest proceeds are presented net in the workbook. The underlying expense stays
			// an expenditure posting for operational reports, but Note 5 displays it as a
			// deduction so its total reconciles to the statement line.
			amountSign = -1
		}
		if category.Amount != 0 || category.PriorAmount != 0 {
			lineName := category.Name
			if amountSign < 0 {
				lineName = "Deduct: " + lineName
			}
			section.Lines = append(section.Lines, NoteLine{
				Name:        lineName,
				Amount:      category.Amount * amountSign,
				PriorAmount: category.PriorAmount * amountSign,
			})
		}
		section.Total += category.Amount * amountSign
		section.PriorTotal += category.PriorAmount * amountSign

		// Append all child categories that belong to this parent. Children with zero
		// activity in both periods are skipped to avoid cluttering the report with
		// empty lines. The child's amounts are added to the section totals regardless
		// of whether the line is displayed.
		for _, child := range childrenByParent[category.ID] {
			if child.Amount == 0 && child.PriorAmount == 0 {
				continue
			}
			section.Lines = append(section.Lines, NoteLine{
				Name:        child.Name,
				Amount:      child.Amount * amountSign,
				PriorAmount: child.PriorAmount * amountSign,
			})
			section.Total += child.Amount * amountSign
			section.PriorTotal += child.PriorAmount * amountSign
		}
	}

	assetSchedule, err := buildFixedAssetData(year)
	if err != nil {
		return NotesData{}, fmt.Errorf("build current Note 21 schedule: %w", err)
	}
	priorAssetSchedule, err := buildFixedAssetData(year - 1)
	if err != nil {
		return NotesData{}, fmt.Errorf("build prior Note 21 schedule: %w", err)
	}
	if expenseSection := sections["21:expenditure"]; expenseSection != nil && expenseSection.Total == 0 && expenseSection.PriorTotal == 0 {
		expenseSection.Lines = []NoteLine{{
			Name:        "Calculated depreciation & amortization charge",
			Amount:      assetSchedule.TotalCharge,
			PriorAmount: priorAssetSchedule.TotalCharge,
		}}
		expenseSection.Total = assetSchedule.TotalCharge
		expenseSection.PriorTotal = priorAssetSchedule.TotalCharge
	}
	if assetSection := sections["21:asset"]; assetSection != nil && (assetSchedule.TotalClosingCost != 0 || priorAssetSchedule.TotalClosingCost != 0) {
		assetSection.Title = "Non-Current Assets Schedule"
		assetSection.Lines = []NoteLine{{Name: "Property, Plant & Equipment — carrying amount", Amount: assetSchedule.PPECarryingAmount, PriorAmount: priorAssetSchedule.PPECarryingAmount}}
		assetSection.Total = assetSchedule.PPECarryingAmount
		assetSection.PriorTotal = priorAssetSchedule.PPECarryingAmount
	}
	if intangibleSection := sections["23"]; intangibleSection != nil && (assetSchedule.TotalClosingCost != 0 || priorAssetSchedule.TotalClosingCost != 0) {
		intangibleSection.Lines = []NoteLine{{Name: "Software — carrying amount", Amount: assetSchedule.IntangibleCarryingAmount, PriorAmount: priorAssetSchedule.IntangibleCarryingAmount}}
		intangibleSection.Total = assetSchedule.IntangibleCarryingAmount
		intangibleSection.PriorTotal = priorAssetSchedule.IntangibleCarryingAmount
	}

	// Transfer the ordered sections into the data struct. Using sectionOrder ensures
	// the report displays sections in the intended sequence (income note_refs 1–9,
	// then expenditure note_refs 10–12) regardless of map iteration order.
	for _, key := range sectionOrder {
		data.Notes = append(data.Notes, *sections[key])
	}

	return data, nil
}

// loadCategoryPositions applies the same opening-plus-movement rule used by the statement
// of financial position, but at individual account level for Notes 21–28. A configured
// opening supersedes prior transaction history; accounts without one retain the legacy
// cumulative calculation for backward compatibility.
func loadCategoryPositions(year int) (map[int64]float64, error) {
	startDate, endDate := yearBounds(year)
	rows, err := db.DB.Query(`
		SELECT c.id,
			COALESCE(opening.amount, 0),
			CASE WHEN opening.category_id IS NULL THEN 0 ELSE 1 END,
			COALESCE(SUM(CASE WHEN t.date >= ? AND t.date < ? THEN t.amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN t.date < ? THEN t.amount ELSE 0 END), 0)
		FROM categories c
		LEFT JOIN account_opening_balances opening ON opening.category_id = c.id AND opening.year = ?
		LEFT JOIN transactions t ON t.category_id = c.id AND t.type = c.type AND t.date < ?
		WHERE c.type IN ('asset', 'liability')
		GROUP BY c.id, opening.category_id, opening.amount
	`, startDate, endDate, endDate, year, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	positions := map[int64]float64{}
	for rows.Next() {
		var id int64
		var opening, movement, cumulative float64
		var configured int
		if err := rows.Scan(&id, &opening, &configured, &movement, &cumulative); err != nil {
			return nil, err
		}
		positions[id] = cumulative
		if configured == 1 {
			positions[id] = opening + movement
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Retain the original Bank/Cash/Momo opening setup for upgraded databases. The standard
	// chart now nests these accounts under Note 26, but their durable names still let us map
	// each legacy opening to the correct account without altering historical transactions.
	legacyRows, err := db.DB.Query(`
		SELECT account_type, amount
		FROM opening_balances
		WHERE year = ?
	`, year)
	if err != nil {
		return nil, err
	}
	type legacyOpening struct {
		accountType string
		amount      float64
	}
	var legacyOpenings []legacyOpening
	for legacyRows.Next() {
		var opening legacyOpening
		if err := legacyRows.Scan(&opening.accountType, &opening.amount); err != nil {
			legacyRows.Close()
			return nil, err
		}
		legacyOpenings = append(legacyOpenings, opening)
	}
	if err := legacyRows.Close(); err != nil {
		return nil, err
	}

	for _, opening := range legacyOpenings {
		accountName := accountLabel(opening.accountType)
		var categoryID int64
		if err := db.DB.QueryRow(`
			SELECT id FROM categories
			WHERE type = 'asset' AND name = ?
			ORDER BY CASE WHEN parent_id <> 0 THEN 0 ELSE 1 END, id
			LIMIT 1
		`, accountName).Scan(&categoryID); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return nil, err
		}
		var explicitOpeningCount int
		if err := db.DB.QueryRow(`
			SELECT COUNT(*) FROM account_opening_balances
			WHERE year = ? AND category_id = ?
		`, year, categoryID).Scan(&explicitOpeningCount); err != nil {
			return nil, err
		}
		if explicitOpeningCount > 0 {
			continue
		}
		var movement float64
		if err := db.DB.QueryRow(`
			SELECT COALESCE(SUM(amount), 0)
			FROM transactions
			WHERE type = 'asset' AND category_id = ? AND date >= ? AND date < ?
		`, categoryID, startDate, endDate).Scan(&movement); err != nil {
			return nil, err
		}
		positions[categoryID] = opening.amount + movement
	}
	return positions, nil
}

// noteTitle resolves a note_ref code to a human-readable section title for the notes
// report. It uses a hard-coded lookup table that maps the numeric note_ref values assigned
// in the category seed data to their corresponding financial reporting titles. If a
// note_ref is not found in the table (which could happen if custom categories are added
// with new note_ref values), the function falls back to the category's own display name,
// ensuring that every note section has some visible title.
func noteTitle(noteRef, fallback string) string {
	// These titles correspond to the updated PCG standard workbook. Notes 3–8 cover income,
	// 9–21 cover expenditure and non-current assets, and 22–28 cover the remaining statement
	// of financial position disclosures.
	titles := map[string]string{
		"3":  "Tithes",
		"4":  "Offerings",
		"5":  "Harvest Proceeds (Net)",
		"6":  "Donations Received",
		"7":  "Investment Income",
		"8":  "Other Income",
		"9":  "Contributions Paid",
		"10": "Agents' Expenses",
		"11": "Staff Cost",
		"12": "Other Allowances",
		"13": "Evangelism Expenses",
		"14": "Group & Committee Expenses",
		"15": "Meetings and Conferences",
		"16": "Training, Seminars, Workshops & Retreats",
		"17": "Social Services",
		"18": "Levies Paid",
		"19": "Property Upkeep",
		"20": "General Administration Expenses",
		"21": "Property, Plant, Equipment, Depreciation & Amortization",
		"22": "Long Term Investment",
		"23": "Intangible Assets",
		"24": "Inventories",
		"25": "Accounts Receivable & Prepayments",
		"26": "Cash & Cash Equivalents",
		"27": "Long Term Loan",
		"28": "Accounts Payable & Accruals",
	}
	if title, ok := titles[noteRef]; ok {
		return title
	}
	// Fallback: use the category name as the section title. This handles edge cases
	// where custom categories have note_ref values not in the lookup table.
	return fallback
}
