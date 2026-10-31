package finance

import (
	"bytes"
	"fmt"
	"net/http"
	"path/filepath"
	"ramseyer-finance/internal/db"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

func BulkImportTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Invalid form submission", http.StatusBadRequest)
		return
	}

	transactionType := strings.TrimSpace(r.FormValue("type"))
	switch transactionType {
	case "income", "expenditure", "asset", "liability":
	default:
		http.Error(w, "Invalid transaction type", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Excel file required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".xlsx" && ext != ".xls" {
		http.Error(w, "Only .xlsx or .xls files are supported", http.StatusBadRequest)
		return
	}

	data := make([]byte, 32<<20)
	n, _ := file.Read(data)
	f, err := excelize.OpenReader(bytes.NewReader(data[:n]))
	if err != nil {
		http.Error(w, "Unable to read Excel file", http.StatusBadRequest)
		return
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		http.Error(w, "No worksheet found", http.StatusBadRequest)
		return
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		http.Error(w, "Unable to read worksheet", http.StatusBadRequest)
		return
	}
	if len(rows) == 0 {
		http.Error(w, "Empty worksheet", http.StatusBadRequest)
		return
	}

	startIdx := 0
	for i := 0; i < len(rows) && i < 10; i++ {
		if len(rows[i]) > 0 {
			for _, c := range rows[i] {
				if strings.Contains(strings.ToLower(c), "date") {
					startIdx = i
					goto foundHeader
				}
			}
		}
	}
	startIdx = 0
foundHeader:

	var count int
	for i := startIdx + 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) == 0 {
			continue
		}
		allEmpty := true
		for _, c := range row {
			if strings.TrimSpace(c) != "" {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			continue
		}

		dateStr := getCellBulk(row, 0)
		amountStr := getCellBulk(row, 1)
		desc := getCellBulk(row, 2)
		categoryName := getCellBulk(row, 3)

		if dateStr == "" || amountStr == "" {
			continue
		}

		parsedDate, err := time.Parse("2006-01-02", strings.TrimSpace(dateStr))
		if err != nil {
			if serial, err2 := strconv.ParseFloat(strings.TrimSpace(dateStr), 64); err2 == nil {
				if t, err3 := excelize.ExcelDateToTime(serial, false); err3 == nil {
					parsedDate = t
				} else {
					continue
				}
			} else {
				continue
			}
		}
		amount, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(amountStr, ",", "")), 64)
		if err != nil {
			continue
		}

		var categoryID int64
		if categoryName != "" {
			err = db.DB.QueryRow(`SELECT id FROM categories WHERE type=? AND name=? AND COALESCE(is_active,1)=1 ORDER BY parent_id DESC LIMIT 1`, transactionType, strings.TrimSpace(categoryName)).Scan(&categoryID)
			if err != nil {
				continue
			}
		} else {
			continue
		}

		paymentMethod := "cash"
		if pm := strings.TrimSpace(getCellBulk(row, 4)); pm != "" {
			pm = strings.ToLower(pm)
			switch pm {
			case "cash", "momo", "cheque", "bank":
				paymentMethod = pm
			}
		}

		_, err = db.DB.Exec(`
			INSERT INTO financial_postings (date, account_type, category_id, payment_method, description, amount, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, parsedDate.Format("2006-01-02"), transactionType, categoryID, paymentMethod, strings.TrimSpace(desc), amount, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			continue
		}
		count++
	}

	http.Redirect(w, r, fmt.Sprintf("/data-entry?tab=%s&msg=Bulk+import+completed+%d+entries", transactionType, count), http.StatusSeeOther)
}

func getCellBulk(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return row[idx]
}

func DownloadDataEntryTemplate(w http.ResponseWriter, r *http.Request) {
	transactionType := strings.TrimSpace(r.URL.Query().Get("type"))
	switch transactionType {
	case "income", "expenditure", "asset", "liability":
	default:
		transactionType = "expenditure"
	}

	f := excelize.NewFile()
	defer f.Close()
	sheet := f.GetSheetName(0)
	_ = f.SetSheetName(sheet, transactionType)

	headers := []string{"Date (YYYY-MM-DD)", "Amount", "Description", "Category", "Payment Method (cash/momo/cheque/bank)"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(transactionType, cell, h)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		http.Error(w, "Failed to generate template", http.StatusInternalServerError)
		return
	}
	filename := fmt.Sprintf("data-entry-%s-template.xlsx", transactionType)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
