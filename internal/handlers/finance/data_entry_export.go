package finance

import (
	"bytes"
	"fmt"
	"net/http"
	"ramseyer-finance/internal/db"
	expenseworkbook "ramseyer-finance/internal/finance/dataentry/expense/workbook"
	"strings"
)

// ExportDataEntryExcel writes saved expenditure entries in the client's workbook shape.
// The client shares this file with the church office, so the sheet name, blank first row,
// headers, and column order all match EXPENDITURE FOR <year>.xlsx instead of using the
// app's internal export format.
func ExportDataEntryExcel(w http.ResponseWriter, r *http.Request) {
	transactionType := strings.TrimSpace(r.URL.Query().Get("type"))
	if transactionType != "expenditure" {
		badRequest(w, "Excel export is available for expenditure entries only")
		return
	}

	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	rows, err := loadExpenseExportRows(transactionType, year)
	if err != nil {
		serverError(w, err)
		return
	}

	workbook, err := expenseworkbook.Build(year, rows)
	if err != nil {
		serverError(w, err)
		return
	}
	defer workbook.Close()

	var buffer bytes.Buffer
	if err := workbook.Write(&buffer); err != nil {
		serverError(w, err)
		return
	}

	filename := fmt.Sprintf("EXPENDITURE FOR %d.xlsx", year)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buffer.Bytes())
}

// loadExpenseExportRows reads the saved postings in date order. The category falls back to
// the joined account name for older rows that predate the denormalised column, so the
// workbook never leaves the account label blank.
func loadExpenseExportRows(transactionType string, year int) ([]expenseworkbook.Row, error) {
	startDate, endDate := yearBounds(year)
	rows, err := db.DB.Query(`
		SELECT t.date, t.description, COALESCE(NULLIF(t.category, ''), c.name, ''), t.amount
		FROM transactions t
		LEFT JOIN categories c ON c.id = t.category_id
		WHERE t.type = ? AND t.date >= ? AND t.date < ?
		ORDER BY t.date ASC, t.id ASC
	`, transactionType, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("load expenditure export rows: %w", err)
	}
	defer rows.Close()

	var entries []expenseworkbook.Row
	for rows.Next() {
		var entry expenseworkbook.Row
		if err := rows.Scan(&entry.Date, &entry.Description, &entry.Category, &entry.Amount); err != nil {
			return nil, fmt.Errorf("scan expenditure export row: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expenditure export rows: %w", err)
	}
	return entries, nil
}
