package finance

import (
	"bytes"
	"fmt"
	"net/http"
	dataentrybulk "ramseyer-finance/internal/finance/dataentry/bulk"
	"strings"

	"github.com/xuri/excelize/v2"
)

// DownloadDataEntryTemplate hands the operator a workbook in the exact shape the importer
// reads. Expenditure uses the client's own layout so the shared file looks like the church
// workbook; the other types keep the generic header row because their data has no client
// template to match.
func DownloadDataEntryTemplate(w http.ResponseWriter, r *http.Request) {
	transactionType := strings.TrimSpace(r.URL.Query().Get("type"))
	switch transactionType {
	case "income", "expenditure", "asset", "liability", "all":
	default:
		transactionType = "expenditure"
	}

	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	var workbook *excelize.File
	if transactionType == "all" {
		workbook, err = dataentrybulk.BuildCombinedTemplate()
	} else {
		var categories []dataentrybulk.Category
		categories, err = loadBulkCategories(transactionType)
		if err == nil {
			if transactionType == "expenditure" {
				workbook, err = dataentrybulk.BuildExpenseWorkbook(year, nil)
				if err == nil {
					err = dataentrybulk.AddEntryTemplateInstructions(workbook, transactionType, categories)
				}
			} else {
				workbook, err = dataentrybulk.BuildEntryTemplate(transactionType, categories)
			}
		}
	}
	if err != nil {
		serverError(w, err)
		return
	}
	defer workbook.Close()

	var buffer bytes.Buffer
	if err := workbook.Write(&buffer); err != nil {
		http.Error(w, "Failed to generate template", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("ramseyer-%s-entry-template.xlsx", transactionType)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buffer.Bytes())
}
