package finance

import (
	"fmt"
	"math"
	"net/http"
	"ramseyer-finance/internal/db"
	"strconv"
	"strings"
)

// TrialBalanceLine is one year-owned account balance. AccountType determines how the
// debit/credit is presented in Notes and the statements; it is deliberately stored on the
// line instead of inferred from a global category because a later year's workbook may use
// a different account list or mapping.
type TrialBalanceLine struct {
	ID          int64
	AccountType string
	Note        string
	Account     string
	Debit       float64
	Credit      float64
	Amount      float64
	NormalSide  string
	IsCustom    bool
}

// TrialBalanceGroup is the digital equivalent of a workbook note section. The operator
// sees the accounting destination once above a group instead of choosing the same type and
// note on every row, which both clarifies the flow and keeps the large entry page responsive.
type TrialBalanceGroup struct {
	Key         string
	AccountType string
	TypeLabel   string
	Note        string
	Title       string
	Lines       []TrialBalanceLine
}

type TrialBalanceData struct {
	Active      string
	Year        string
	Years       []int
	Lines       []TrialBalanceLine
	Groups      []TrialBalanceGroup
	TotalDebit  float64
	TotalCredit float64
	Difference  float64
	Saved       bool
	Added       bool
}

func TrialBalance(w http.ResponseWriter, r *http.Request) {
	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	data, err := buildTrialBalanceData(year)
	if err != nil {
		serverError(w, err)
		return
	}
	data.Saved = r.URL.Query().Get("saved") == "1"
	data.Added = r.URL.Query().Get("added") == "1"
	RenderTemplate(w, "trial-balance", data)
}

// SaveTrialBalance stores one signed, natural balance for every visible account. Assets and
// expenditure normally post to debit while income, liabilities, and funds normally post to
// credit; a negative value intentionally creates a contra balance. Keeping that accounting
// rule on the server lets the entry page match the workbook's single-amount workflow without
// weakening the debit/credit controls used by reports and balancing checks.
func SaveTrialBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid Trial Balance form")
		return
	}
	year, err := parseReportYear(r.FormValue("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := ensureTrialBalanceYear(year); err != nil {
		serverError(w, err)
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback()
	accountTypes := make(map[int64]string)
	rows, err := tx.Query("SELECT id, account_type FROM trial_balance_entries WHERE year = ?", year)
	if err != nil {
		serverError(w, fmt.Errorf("load Trial Balance row types: %w", err))
		return
	}
	for rows.Next() {
		var id int64
		var accountType string
		if err := rows.Scan(&id, &accountType); err != nil {
			rows.Close()
			serverError(w, fmt.Errorf("scan Trial Balance row type: %w", err))
			return
		}
		accountTypes[id] = accountType
	}
	if err := rows.Close(); err != nil {
		serverError(w, fmt.Errorf("close Trial Balance row types: %w", err))
		return
	}
	for _, rawID := range r.Form["entry_id"] {
		id, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || id <= 0 {
			badRequest(w, "Invalid Trial Balance row")
			return
		}
		accountType, exists := accountTypes[id]
		if !exists || !validTrialBalanceType(accountType) {
			badRequest(w, "A Trial Balance row no longer exists for this year")
			return
		}
		amount, amountErr := parseSignedMoney(r.FormValue(fmt.Sprintf("amount_%d", id)))
		if amountErr != nil {
			badRequest(w, "Every Trial Balance amount must be a valid number")
			return
		}
		line := TrialBalanceLine{AccountType: accountType}
		assignNaturalBalance(&line, amount)
		result, err := tx.Exec(`
			UPDATE trial_balance_entries
			SET debit = ?, credit = ?, updated_at = datetime('now','localtime')
			WHERE id = ? AND year = ?
		`, line.Debit, line.Credit, id, year)
		if err != nil {
			serverError(w, fmt.Errorf("save Trial Balance row: %w", err))
			return
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			badRequest(w, "A Trial Balance row no longer exists for this year")
			return
		}
	}
	if _, err := tx.Exec("UPDATE trial_balance_years SET updated_at = datetime('now','localtime') WHERE year = ?", year); err != nil {
		serverError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trial-balance?year=%d&saved=1", year), http.StatusSeeOther)
}

// AddTrialBalanceLine creates a row only inside the selected year. This is the mechanism
// that lets 2027 diverge from 2026 without altering the chart or historical statements.
func AddTrialBalanceLine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	year, err := parseReportYear(r.FormValue("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	accountType := strings.TrimSpace(r.FormValue("account_type"))
	accountName := strings.TrimSpace(r.FormValue("account_name"))
	noteRef := strings.TrimSpace(r.FormValue("note_ref"))
	if !validTrialBalanceType(accountType) || accountName == "" || !validTrialBalanceNote(noteRef) {
		badRequest(w, "Choose a valid type and note, then enter an account name")
		return
	}
	if err := ensureTrialBalanceYear(year); err != nil {
		serverError(w, err)
		return
	}
	var nextSort int
	if err := db.DB.QueryRow("SELECT COALESCE(MAX(sort_order), 0) + 1 FROM trial_balance_entries WHERE year = ?", year).Scan(&nextSort); err != nil {
		serverError(w, err)
		return
	}
	if _, err := db.DB.Exec(`
		INSERT INTO trial_balance_entries (year, account_type, note_ref, account_name, sort_order)
		VALUES (?, ?, ?, ?, ?)
	`, year, accountType, noteRef, accountName, nextSort); err != nil {
		serverError(w, fmt.Errorf("add Trial Balance row: %w", err))
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trial-balance?year=%d&added=1", year), http.StatusSeeOther)
}

func DeleteTrialBalanceLine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	year, err := parseReportYear(r.FormValue("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	if err != nil || id <= 0 {
		badRequest(w, "Invalid Trial Balance row")
		return
	}
	if _, err := db.DB.Exec("DELETE FROM trial_balance_entries WHERE id = ? AND year = ?", id, year); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trial-balance?year=%d", year), http.StatusSeeOther)
}

// buildTrialBalanceData reads only the selected year's saved rows. The initializer takes a
// one-time snapshot from legacy transaction data for upgraded installations, after which
// transaction changes cannot silently rewrite a Trial Balance the operator has reviewed.
func buildTrialBalanceData(year int) (TrialBalanceData, error) {
	if err := ensureTrialBalanceYear(year); err != nil {
		return TrialBalanceData{}, err
	}
	years, err := reportYears(year)
	if err != nil {
		return TrialBalanceData{}, err
	}
	data := TrialBalanceData{Active: "trial-balance", Year: strconv.Itoa(year), Years: years}
	rows, err := db.DB.Query(`
		SELECT id, account_type, note_ref, account_name, debit, credit,
			CASE WHEN source_category_id IS NULL THEN 1 ELSE 0 END
		FROM trial_balance_entries
		WHERE year = ?
		ORDER BY CAST(NULLIF(note_ref, '') AS INTEGER),
			CASE account_type WHEN 'income' THEN 0 WHEN 'expenditure' THEN 1 WHEN 'asset' THEN 2 WHEN 'liability' THEN 3 ELSE 4 END,
			sort_order, id
	`, year)
	if err != nil {
		return TrialBalanceData{}, fmt.Errorf("query Trial Balance rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var line TrialBalanceLine
		if err := rows.Scan(&line.ID, &line.AccountType, &line.Note, &line.Account, &line.Debit, &line.Credit, &line.IsCustom); err != nil {
			return TrialBalanceData{}, fmt.Errorf("scan Trial Balance row: %w", err)
		}
		line.Amount = trialBalanceAmount(line.AccountType, line.Debit, line.Credit)
		line.NormalSide = trialBalanceNormalSide(line.AccountType)
		data.Lines = append(data.Lines, line)
		data.TotalDebit += line.Debit
		data.TotalCredit += line.Credit

		groupKey := line.Note + ":" + line.AccountType
		if len(data.Groups) == 0 || data.Groups[len(data.Groups)-1].Key != groupKey {
			data.Groups = append(data.Groups, TrialBalanceGroup{
				Key:         groupKey,
				AccountType: line.AccountType,
				TypeLabel:   trialBalanceTypeLabel(line.AccountType),
				Note:        line.Note,
				Title:       noteTitle(line.Note, "Unmapped accounts"),
			})
		}
		lastGroup := &data.Groups[len(data.Groups)-1]
		lastGroup.Lines = append(lastGroup.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return TrialBalanceData{}, err
	}
	data.Difference = data.TotalDebit - data.TotalCredit
	return data, nil
}

func parseNonNegativeMoney(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", ""), 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid amount")
	}
	return value, nil
}

func parseSignedMoney(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", ""), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("invalid amount")
	}
	return value, nil
}

func trialBalanceNormalSide(accountType string) string {
	if accountType == "asset" || accountType == "expenditure" {
		return "Debit"
	}
	return "Credit"
}

func trialBalanceTypeLabel(accountType string) string {
	switch accountType {
	case "income":
		return "Income"
	case "expenditure":
		return "Expenditure"
	case "asset":
		return "Asset"
	case "liability":
		return "Liability"
	case "equity":
		return "Equity / Fund"
	default:
		return accountType
	}
}

func validTrialBalanceType(value string) bool {
	switch value {
	case "income", "expenditure", "asset", "liability", "equity":
		return true
	default:
		return false
	}
}

func validTrialBalanceNote(value string) bool {
	if value == "" {
		return true
	}
	note, err := strconv.Atoi(value)
	return err == nil && note >= 3 && note <= 28
}
