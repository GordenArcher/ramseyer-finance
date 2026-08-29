package finance

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"ramseyer-finance/internal/db"
	"strconv"
	"strings"
)

type ManagedCategory struct {
	ID               int64
	Type             string
	Name             string
	ParentID         int64
	ParentName       string
	NoteRef          string
	ReportSection    string
	IsActive         bool
	TransactionCount int
	BudgetCount      int
}

// CategoriesPageData carries the dedicated category-management screen state. I keep it
// separate from SetupData because category administration now has its own route, table,
// and modal flow, and I do not want the setup page model to keep growing with unrelated
// concerns.
type CategoriesPageData struct {
	Active            string
	Message           string
	MessageTone       string
	Page              int
	TotalPages        int
	TotalCount        int
	HasPrev           bool
	HasNext           bool
	PrevPageURL       string
	NextPageURL       string
	PageLinks         []PageLink
	ReturnTo          string
	ManagedCategories []ManagedCategory
	CategoryParents   []CategoryParentOption
	CategoryForm      CategoryFormData
	Search            string
	FilterType        string
	FilterStatus      string
	FilterNote        string
	FilterSection     string
	HasFilters        bool
	NoteFilterOptions []int
}

type categoryFilters struct {
	Search  string
	Type    string
	Status  string
	Note    string
	Section string
}

type CategoryParentOption struct {
	ID    int64
	Type  string
	Label string
}

type CategoryFormData struct {
	Loaded        bool
	ID            int64
	Type          string
	Name          string
	ParentID      int64
	NoteRef       string
	ReportSection string
}

type categoryParentMeta struct {
	ID            int64
	Type          string
	Name          string
	ParentID      int64
	NoteRef       string
	ReportSection string
	IsActive      bool
}

type categoryUsageSummary struct {
	TransactionCount int
	BudgetCount      int
}

// categoryPageSize keeps the category register compact enough to scan on desktop while still
// preventing the page from turning into an endlessly growing sheet once operators start adding
// their own church-specific categories over time.
const categoryPageSize = 20

func CategoriesPage(w http.ResponseWriter, r *http.Request) {
	// I keep category management on its own page because the category register grows over time and
	// competes visually with budgets and backup settings when everything is forced into Setup.
	page := parsePageNumber(r.URL.Query().Get("page"))
	filters := parseCategoryFilters(r)
	managedCategories, totalCount, page, err := loadManagedCategories(page, categoryPageSize, filters)
	if err != nil {
		serverError(w, err)
		return
	}
	categoryParents, err := loadCategoryParentOptions()
	if err != nil {
		serverError(w, err)
		return
	}
	categoryForm, err := loadCategoryFormData(r.URL.Query().Get("edit"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	totalPages := totalPagesForCount(totalCount, categoryPageSize)
	returnTo := buildCategoriesURL(page, filters)
	data := CategoriesPageData{
		Active:            "categories",
		Message:           r.URL.Query().Get("msg"),
		MessageTone:       alertTone(r.URL.Query().Get("msg")),
		Page:              page,
		TotalPages:        totalPages,
		TotalCount:        totalCount,
		HasPrev:           page > 1,
		HasNext:           page < totalPages,
		PageLinks:         buildCategoryPageLinks(page, totalPages, filters),
		ReturnTo:          returnTo,
		ManagedCategories: managedCategories,
		CategoryParents:   categoryParents,
		CategoryForm:      categoryForm,
		Search:            filters.Search,
		FilterType:        filters.Type,
		FilterStatus:      filters.Status,
		FilterNote:        filters.Note,
		FilterSection:     filters.Section,
		HasFilters:        filters != (categoryFilters{}),
		NoteFilterOptions: trialBalanceNoteNumbers(),
	}
	if data.HasPrev {
		data.PrevPageURL = buildCategoriesURL(page-1, filters)
	}
	if data.HasNext {
		data.NextPageURL = buildCategoriesURL(page+1, filters)
	}

	RenderTemplate(w, "categories", data)
}

func trialBalanceNoteNumbers() []int {
	notes := make([]int, 0, 26)
	for note := 3; note <= 28; note++ {
		notes = append(notes, note)
	}
	return notes
}

func SaveCategory(w http.ResponseWriter, r *http.Request) {
	// I keep category writes in a dedicated handler because categories now behave like a live
	// chart-of-accounts configuration, not a fixed seed list. This is the boundary where I validate
	// hierarchy rules, reporting metadata, and archive-safe edits before anything reaches SQLite.
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}
	returnTo := sanitizeReturnTo(r.FormValue("return_to"), buildCategoriesURL(1, categoryFilters{}))

	categoryID, err := parseOptionalInt64(r.FormValue("id"))
	if err != nil || categoryID < 0 {
		badRequest(w, "Invalid category")
		return
	}

	categoryType := strings.TrimSpace(r.FormValue("type"))
	switch categoryType {
	case "income", "expenditure", "asset", "liability":
	default:
		badRequest(w, "Invalid category type")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		badRequest(w, "Category name is required")
		return
	}

	parentID, err := parseOptionalInt64(r.FormValue("parent_id"))
	if err != nil || parentID < 0 {
		badRequest(w, "Invalid parent category")
		return
	}
	if categoryID > 0 && categoryID == parentID {
		badRequest(w, "A category cannot be its own parent")
		return
	}

	reportSection := strings.TrimSpace(r.FormValue("report_section"))
	noteRef := strings.TrimSpace(r.FormValue("note_ref"))

	var existing ManagedCategory
	if categoryID > 0 {
		existing, err = loadManagedCategory(categoryID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				badRequest(w, "Category not found")
				return
			}
			serverError(w, err)
			return
		}

		// I do not allow type changes on existing categories because transactions, budgets,
		// and report structure all depend on that semantic meaning remaining stable.
		if existing.Type != categoryType {
			badRequest(w, "Category type cannot be changed once created")
			return
		}
	}

	var parentMeta categoryParentMeta
	if parentID > 0 {
		parentMeta, err = loadCategoryParentMeta(parentID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				badRequest(w, "Selected parent category was not found")
				return
			}
			serverError(w, err)
			return
		}
		if parentMeta.Type != categoryType {
			badRequest(w, "Parent category must use the same category type")
			return
		}
		if parentMeta.ParentID != 0 {
			badRequest(w, "Only top-level categories can be used as parents")
			return
		}
		if !parentMeta.IsActive {
			badRequest(w, "Parent category must be active before assigning children")
			return
		}

		// I inherit the note mapping from the top-level income/expenditure parent so new
		// child categories automatically flow into the same notes section as their family.
		if categoryType == "income" || categoryType == "expenditure" {
			noteRef = parentMeta.NoteRef
		}
		reportSection = ""
	}

	if categoryID > 0 && existing.ParentID == 0 && parentID > 0 {
		var childCount int
		if err := db.DB.QueryRow(`SELECT COUNT(*) FROM categories WHERE parent_id = ?`, categoryID).Scan(&childCount); err != nil {
			serverError(w, err)
			return
		}
		if childCount > 0 {
			badRequest(w, "A parent category with child categories cannot be converted into a child category")
			return
		}
	}

	if parentID == 0 {
		switch categoryType {
		case "income", "expenditure":
			reportSection = ""
		case "asset":
			if reportSection != "current_asset" && reportSection != "non_current_asset" {
				badRequest(w, "Top-level asset categories must choose either Current Asset or Non-Current Asset")
				return
			}
		case "liability":
			if reportSection == "" {
				reportSection = "current_liability"
			}
			if reportSection != "current_liability" && reportSection != "long_term_liability" {
				badRequest(w, "Top-level liability categories must choose Current Liability or Long-Term Liability")
				return
			}
		}
	}

	if categoryID == 0 {
		// I create new categories as active by default because the usual reason to add a category
		// is that the operator needs to use it immediately in transaction entry.
		_, err = db.DB.Exec(`
			INSERT INTO categories (type, name, parent_id, note_ref, report_section, is_active)
			VALUES (?, ?, ?, ?, ?, 1)
		`, categoryType, name, parentID, noteRef, reportSection)
	} else {
		_, err = db.DB.Exec(`
			UPDATE categories
			SET name = ?, parent_id = ?, note_ref = ?, report_section = ?
			WHERE id = ?
		`, name, parentID, noteRef, reportSection, categoryID)
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			redirectWithMessage(w, r, returnTo, "Category name already exists for that parent")
			return
		}
		serverError(w, err)
		return
	}

	if categoryID == 0 {
		if err := db.DB.QueryRow(`SELECT last_insert_rowid()`).Scan(&categoryID); err != nil {
			serverError(w, err)
			return
		}
	}

	if parentID == 0 && (categoryType == "income" || categoryType == "expenditure") {
		// I propagate top-level note_ref changes down to child rows so future note queries do not
		// depend on every child being edited individually after the parent mapping changes.
		if _, err := db.DB.Exec(`
			UPDATE categories
			SET note_ref = ?
			WHERE parent_id = ? AND type = ?
		`, noteRef, categoryID, categoryType); err != nil {
			serverError(w, err)
			return
		}
	}

	if err := syncTransactionMetadataForCategoryTree(categoryID); err != nil {
		serverError(w, err)
		return
	}

	message := "Category saved"
	if categoryID > 0 && existing.ID > 0 {
		message = "Category updated"
	}
	redirectWithMessage(w, r, returnTo, message)
}

func ToggleCategoryStatus(w http.ResponseWriter, r *http.Request) {
	// I use archive/restore instead of hard delete because categories participate in
	// transactions, budgets, and report structure. Hiding a category from new entry is
	// usually the safe operational need; removing history is not.
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}
	returnTo := sanitizeReturnTo(r.FormValue("return_to"), buildCategoriesURL(1, categoryFilters{}))

	categoryID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	if err != nil || categoryID <= 0 {
		// I redirect back with an inline message here instead of returning a raw 400 body because
		// this action comes from the register UI. Rendering plain text in the webview makes the
		// failure look like a broken navigation rather than a recoverable validation problem.
		redirectWithMessage(w, r, returnTo, "Category action could not be completed. Refresh and try again.")
		return
	}

	category, err := loadManagedCategory(categoryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			redirectWithMessage(w, r, returnTo, "Category not found")
			return
		}
		serverError(w, err)
		return
	}

	newStatus := 0
	message := "Category archived"
	if !category.IsActive {
		newStatus = 1
		message = "Category restored"
	}

	if category.IsActive {
		usage, err := loadCategoryUsageSummary(category)
		if err != nil {
			serverError(w, err)
			return
		}
		if usage.TransactionCount > 0 || usage.BudgetCount > 0 {
			// I block archiving once a category is already tied to live financial history because
			// operators read "archive" as a soft remove. If history still depends on the category,
			// I would rather tell them exactly why the action is being refused than hide it and risk
			// later confusion about where posted items or budgets belong.
			details := []string{}
			if usage.TransactionCount > 0 {
				details = append(details, fmt.Sprintf("%d transaction%s", usage.TransactionCount, pluralSuffix(usage.TransactionCount)))
			}
			if usage.BudgetCount > 0 {
				details = append(details, fmt.Sprintf("%d budget record%s", usage.BudgetCount, pluralSuffix(usage.BudgetCount)))
			}
			redirectWithMessage(w, r, returnTo, "This category is already in use by "+strings.Join(details, " and ")+". Move those records first before archiving it.")
			return
		}
	}

	if _, err := db.DB.Exec(`UPDATE categories SET is_active = ? WHERE id = ?`, newStatus, categoryID); err != nil {
		serverError(w, err)
		return
	}

	if category.ParentID == 0 {
		if _, err := db.DB.Exec(`UPDATE categories SET is_active = ? WHERE parent_id = ?`, newStatus, categoryID); err != nil {
			serverError(w, err)
			return
		}
	} else if newStatus == 1 {
		// I automatically restore the parent when restoring a child because an active child under
		// an archived parent would produce a confusing orphaned hierarchy in the setup screens.
		if _, err := db.DB.Exec(`UPDATE categories SET is_active = 1 WHERE id = ?`, category.ParentID); err != nil {
			serverError(w, err)
			return
		}
	}

	redirectWithMessage(w, r, returnTo, message)
}

func loadManagedCategories(page, pageSize int, filters categoryFilters) ([]ManagedCategory, int, int, error) {
	// I return transaction and budget counts with each category because those usage signals are
	// what make archive decisions safe for the operator. A category with history should not feel
	// like a disposable label.
	where, args := categoryFilterSQL(filters)
	var totalCount int
	countQuery := `SELECT COUNT(*) FROM categories c LEFT JOIN categories parent ON parent.id = c.parent_id ` + where
	if err := db.DB.QueryRow(countQuery, args...).Scan(&totalCount); err != nil {
		return nil, 0, 1, fmt.Errorf("count managed categories: %w", err)
	}

	totalPages := totalPagesForCount(totalCount, pageSize)
	if page > totalPages {
		page = totalPages
	}
	offset := (page - 1) * pageSize

	query := `
		SELECT
			c.id,
			c.type,
			c.name,
			c.parent_id,
			COALESCE(parent.name, ''),
			COALESCE(c.note_ref, ''),
			COALESCE(c.report_section, ''),
			COALESCE(c.is_active, 1),
			(SELECT COUNT(*) FROM transactions t WHERE t.category_id = c.id),
			(SELECT COUNT(*) FROM budgets b WHERE b.category_id = c.id)
		FROM categories c
		LEFT JOIN categories parent ON parent.id = c.parent_id
		` + where + `
		ORDER BY
			CASE c.type
				WHEN 'income' THEN 0
				WHEN 'expenditure' THEN 1
				WHEN 'asset' THEN 2
				WHEN 'liability' THEN 3
				ELSE 4
			END,
			CASE WHEN c.parent_id = 0 THEN c.id ELSE c.parent_id END,
			c.parent_id,
			c.id
		LIMIT ? OFFSET ?
	`
	queryArgs := append(append([]any{}, args...), pageSize, offset)
	rows, err := db.DB.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, page, fmt.Errorf("query managed categories: %w", err)
	}
	defer rows.Close()

	var categories []ManagedCategory
	for rows.Next() {
		var row ManagedCategory
		var isActive int
		if err := rows.Scan(
			&row.ID,
			&row.Type,
			&row.Name,
			&row.ParentID,
			&row.ParentName,
			&row.NoteRef,
			&row.ReportSection,
			&isActive,
			&row.TransactionCount,
			&row.BudgetCount,
		); err != nil {
			return nil, 0, page, fmt.Errorf("scan managed category: %w", err)
		}
		row.IsActive = isActive == 1
		categories = append(categories, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, page, fmt.Errorf("iterate managed categories: %w", err)
	}

	return categories, totalCount, page, nil
}

func loadCategoryParentOptions() ([]CategoryParentOption, error) {
	// I restrict parent options to active top-level categories because the hierarchy in this app
	// is deliberately shallow: one parent level and one child level. Anything deeper would ripple
	// through entry forms and reports for very little practical gain.
	rows, err := db.DB.Query(`
		SELECT id, type, name
		FROM categories
		WHERE parent_id = 0 AND COALESCE(is_active, 1) = 1
		ORDER BY
			CASE type
				WHEN 'income' THEN 0
				WHEN 'expenditure' THEN 1
				WHEN 'asset' THEN 2
				WHEN 'liability' THEN 3
				ELSE 4
			END,
			id
	`)
	if err != nil {
		return nil, fmt.Errorf("query category parent options: %w", err)
	}
	defer rows.Close()

	var options []CategoryParentOption
	for rows.Next() {
		var option CategoryParentOption
		var name string
		if err := rows.Scan(&option.ID, &option.Type, &name); err != nil {
			return nil, fmt.Errorf("scan category parent option: %w", err)
		}
		option.Label = categoryTypeLabel(option.Type) + ": " + name
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate category parent options: %w", err)
	}

	return options, nil
}

func loadCategoryFormData(rawID string) (CategoryFormData, error) {
	// I load edit state by id from the query string so the page can reopen directly into the
	// modal editor after navigation. That keeps edit actions simple and lets the shared modal
	// system stay declarative.
	value := strings.TrimSpace(rawID)
	if value == "" {
		return CategoryFormData{}, nil
	}

	categoryID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || categoryID <= 0 {
		return CategoryFormData{}, fmt.Errorf("invalid category edit target")
	}

	category, err := loadManagedCategory(categoryID)
	if err != nil {
		return CategoryFormData{}, err
	}

	return CategoryFormData{
		Loaded:        true,
		ID:            category.ID,
		Type:          category.Type,
		Name:          category.Name,
		ParentID:      category.ParentID,
		NoteRef:       category.NoteRef,
		ReportSection: category.ReportSection,
	}, nil
}

func loadManagedCategory(categoryID int64) (ManagedCategory, error) {
	// I keep a single detailed loader for one category because save/toggle operations need the
	// same joined metadata and usage counts that the table itself shows.
	var category ManagedCategory
	var isActive int
	err := db.DB.QueryRow(`
		SELECT
			c.id,
			c.type,
			c.name,
			c.parent_id,
			COALESCE(parent.name, ''),
			COALESCE(c.note_ref, ''),
			COALESCE(c.report_section, ''),
			COALESCE(c.is_active, 1),
			(SELECT COUNT(*) FROM transactions t WHERE t.category_id = c.id),
			(SELECT COUNT(*) FROM budgets b WHERE b.category_id = c.id)
		FROM categories c
		LEFT JOIN categories parent ON parent.id = c.parent_id
		WHERE c.id = ?
	`, categoryID).Scan(
		&category.ID,
		&category.Type,
		&category.Name,
		&category.ParentID,
		&category.ParentName,
		&category.NoteRef,
		&category.ReportSection,
		&isActive,
		&category.TransactionCount,
		&category.BudgetCount,
	)
	if err != nil {
		return ManagedCategory{}, err
	}
	category.IsActive = isActive == 1
	return category, nil
}

func loadCategoryUsageSummary(category ManagedCategory) (categoryUsageSummary, error) {
	// I count both the selected category and, for top-level rows, their immediate children because
	// the category model in this app is intentionally two-tiered. That makes the archive decision
	// match what the operator sees as one family in the register rather than only one raw row id.
	query := `
		SELECT
			(SELECT COUNT(*) FROM transactions
				WHERE category_id = c.id
					OR (? = 0 AND category_id IN (SELECT id FROM categories WHERE parent_id = c.id))),
			(SELECT COUNT(*) FROM budgets WHERE category_id = c.id OR (? = 0 AND category_id IN (SELECT id FROM categories WHERE parent_id = c.id)))
		FROM categories c
		WHERE c.id = ?
	`

	var summary categoryUsageSummary
	if err := db.DB.QueryRow(query, category.ParentID, category.ParentID, category.ID).Scan(&summary.TransactionCount, &summary.BudgetCount); err != nil {
		return categoryUsageSummary{}, fmt.Errorf("load category usage summary: %w", err)
	}
	return summary, nil
}

func loadCategoryParentMeta(categoryID int64) (categoryParentMeta, error) {
	// I separate the lighter parent lookup from the full managed-category loader because save
	// validation only needs hierarchy and reporting metadata here, not usage counters.
	var meta categoryParentMeta
	var isActive int
	err := db.DB.QueryRow(`
		SELECT id, type, name, parent_id, COALESCE(note_ref, ''), COALESCE(report_section, ''), COALESCE(is_active, 1)
		FROM categories
		WHERE id = ?
	`, categoryID).Scan(
		&meta.ID,
		&meta.Type,
		&meta.Name,
		&meta.ParentID,
		&meta.NoteRef,
		&meta.ReportSection,
		&isActive,
	)
	if err != nil {
		return categoryParentMeta{}, err
	}
	meta.IsActive = isActive == 1
	return meta, nil
}

func parseOptionalInt64(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func categoryTypeLabel(categoryType string) string {
	switch categoryType {
	case "income":
		return "Income"
	case "expenditure":
		return "Expenditure"
	case "asset":
		return "Asset"
	case "liability":
		return "Liability"
	default:
		return categoryType
	}
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// buildCategoriesURL preserves the paginated register location so archive, edit, and save
// flows can always return the operator to the same slice of the chart-of-accounts register.
func buildCategoriesURL(page int, filters categoryFilters) string {
	values := url.Values{}
	values.Set("page", strconv.Itoa(page))
	if filters.Search != "" {
		values.Set("q", filters.Search)
	}
	if filters.Type != "" {
		values.Set("type", filters.Type)
	}
	if filters.Status != "" {
		values.Set("status", filters.Status)
	}
	if filters.Note != "" {
		values.Set("note", filters.Note)
	}
	if filters.Section != "" {
		values.Set("section", filters.Section)
	}
	return "/categories?" + values.Encode()
}

// buildCategoryPageLinks mirrors the transaction register pagination window so both long
// operational tables behave the same way instead of teaching users two different paging
// patterns for two different registers.
func buildCategoryPageLinks(page, totalPages int, filters categoryFilters) []PageLink {
	if totalPages <= 1 {
		return nil
	}

	start := page - 2
	if start < 1 {
		start = 1
	}
	end := start + 4
	if end > totalPages {
		end = totalPages
	}
	if end-start < 4 {
		start = end - 4
		if start < 1 {
			start = 1
		}
	}

	links := make([]PageLink, 0, end-start+1)
	for number := start; number <= end; number++ {
		links = append(links, PageLink{
			Number: number,
			URL:    buildCategoriesURL(number, filters),
			Active: number == page,
		})
	}
	return links
}

// parseCategoryFilters accepts only the filter vocabulary rendered by the categories page.
// Normalising here keeps pagination URLs stable and prevents arbitrary values from producing
// confusing empty registers that cannot be reproduced through the visible controls.
func parseCategoryFilters(r *http.Request) categoryFilters {
	filters := categoryFilters{Search: strings.TrimSpace(r.URL.Query().Get("q"))}
	if value := strings.TrimSpace(r.URL.Query().Get("type")); value == "income" || value == "expenditure" || value == "asset" || value == "liability" {
		filters.Type = value
	}
	if value := strings.TrimSpace(r.URL.Query().Get("status")); value == "active" || value == "archived" {
		filters.Status = value
	}
	if value := strings.TrimSpace(r.URL.Query().Get("note")); value != "" {
		if note, err := strconv.Atoi(value); err == nil && note >= 3 && note <= 28 {
			filters.Note = value
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("section")); value == "current_asset" || value == "non_current_asset" || value == "current_liability" || value == "long_term_liability" {
		filters.Section = value
	}
	return filters
}

func categoryFilterSQL(filters categoryFilters) (string, []any) {
	searchPattern := "%" + strings.ToLower(filters.Search) + "%"
	where := `WHERE
		(? = '' OR lower(c.name) LIKE ? OR lower(COALESCE(parent.name, '')) LIKE ? OR c.note_ref LIKE ?)
		AND (? = '' OR c.type = ?)
		AND (? = '' OR (? = 'active' AND COALESCE(c.is_active, 1) = 1) OR (? = 'archived' AND COALESCE(c.is_active, 1) = 0))
		AND (? = '' OR c.note_ref = ?)
		AND (? = '' OR c.report_section = ?)`
	args := []any{
		filters.Search, searchPattern, searchPattern, searchPattern,
		filters.Type, filters.Type,
		filters.Status, filters.Status, filters.Status,
		filters.Note, filters.Note,
		filters.Section, filters.Section,
	}
	return where, args
}

func syncTransactionMetadataForCategoryTree(categoryID int64) error {
	// I resync denormalised transaction metadata immediately after category changes so renamed
	// categories and note-ref edits show up in the register and reports without requiring a restart.
	_, err := db.DB.Exec(`
		UPDATE transactions
		SET
			category = COALESCE((
				SELECT c.name
				FROM categories c
				WHERE c.id = transactions.category_id
				LIMIT 1
			), category),
			note_ref = COALESCE((
				SELECT c.note_ref
				FROM categories c
				WHERE c.id = transactions.category_id
				LIMIT 1
			), note_ref)
		WHERE category_id = ? OR category_id IN (
			SELECT id FROM categories WHERE parent_id = ?
		)
	`, categoryID, categoryID)
	if err != nil {
		return fmt.Errorf("sync transaction metadata for category tree: %w", err)
	}
	return nil
}
