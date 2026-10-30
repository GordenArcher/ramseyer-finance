package finance

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// ExportReportExcel builds a SpreadsheetML Excel document on the Go side.
func ExportReportExcel(w http.ResponseWriter, r *http.Request) {
	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	report := strings.TrimSpace(r.URL.Query().Get("report"))
	content, filename, err := buildReportExcel(report, year)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if filename == "" {
		filename = fmt.Sprintf("ramseyer-%s-%d.xls", strings.ReplaceAll(report, "_", "-"), year)
	}
	w.Header().Set("Content-Type", "application/vnd.ms-excel")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func buildReportExcel(report string, year int) ([]byte, string, error) {
	switch report {
	case "trial-balance":
		data, err := buildTrialBalanceData(year)
		if err != nil {
			return nil, "", err
		}
		var buffer bytes.Buffer
		writeExcelHeader(&buffer, "RAMSEYER PRESBYTERIAN CHURCH - AFIAMAN", "TRIAL BALANCE", year)
		appendExcelRow(&buffer, []string{"Note", "Type", "Account", "Amount (GHS)"})
		var total float64
		for _, row := range data.Lines {
			appendExcelRow(&buffer, []string{
				row.Note,
				row.AccountType,
				row.Account,
				formatExcelAmount(row.Amount),
			})
			total += row.Amount
		}
		appendExcelRow(&buffer, []string{})
		appendExcelRow(&buffer, []string{"TOTALS", "", "", formatExcelAmount(total)})
		buffer.WriteString(`</Table></Worksheet></Workbook>`)
		return buffer.Bytes(), fmt.Sprintf("ramseyer-trial-balance-%d.xls", year), nil
	case "annual":
		data, err := buildAnnualData(year)
		if err != nil {
			return nil, "", err
		}
		var buffer bytes.Buffer
		writeExcelHeader(&buffer, "RAMSEYER PRESBYTERIAN CHURCH - AFIAMAN", "ANNUAL FINANCIAL PERFORMANCE", year)
		appendExcelRow(&buffer, []string{"Note", "Description", fmt.Sprintf("%d", year-1), fmt.Sprintf("%d", year)})
		appendExcelRow(&buffer, []string{"INCOME", "", "", ""})
		for _, row := range data.IncomeLines {
			appendExcelRow(&buffer, []string{row.Note, row.Name, formatExcelAmount(row.PriorAmount), formatExcelAmount(row.Amount)})
		}
		appendExcelRow(&buffer, []string{"EXPENDITURE", "", "", ""})
		for _, row := range data.ExpenseLines {
			appendExcelRow(&buffer, []string{row.Note, row.Name, formatExcelAmount(row.PriorAmount), formatExcelAmount(row.Amount)})
		}
		appendExcelRow(&buffer, []string{"SURPLUS / (DEFICIT)", "", formatExcelAmount(data.PriorSurplus), formatExcelAmount(data.Surplus)})
		buffer.WriteString(`</Table></Worksheet></Workbook>`)
		return buffer.Bytes(), fmt.Sprintf("ramseyer-annual-%d.xls", year), nil
	case "balance-sheet":
		data, err := buildBalanceData(year)
		if err != nil {
			return nil, "", err
		}
		var buffer bytes.Buffer
		writeExcelHeader(&buffer, "RAMSEYER PRESBYTERIAN CHURCH - AFIAMAN", "STATEMENT OF FINANCIAL POSITION", year)
		appendExcelRow(&buffer, []string{"Description", fmt.Sprintf("%d", year-1), fmt.Sprintf("%d", year)})
		appendExcelRow(&buffer, []string{"NON-CURRENT ASSETS", "", ""})
		for _, row := range data.NonCurrentAssets {
			appendExcelRow(&buffer, []string{row.Name, formatExcelAmount(row.PriorAmount), formatExcelAmount(row.Amount)})
		}
		appendExcelRow(&buffer, []string{"CURRENT ASSETS", "", ""})
		for _, row := range data.CurrentAssets {
			appendExcelRow(&buffer, []string{row.Name, formatExcelAmount(row.PriorAmount), formatExcelAmount(row.Amount)})
		}
		appendExcelRow(&buffer, []string{"TOTAL ASSETS", formatExcelAmount(data.PriorTotalAssets), formatExcelAmount(data.TotalAssets)})
		appendExcelRow(&buffer, []string{"LIABILITIES", "", ""})
		for _, row := range data.LongTermLiabilities {
			appendExcelRow(&buffer, []string{row.Name, formatExcelAmount(row.PriorAmount), formatExcelAmount(row.Amount)})
		}
		for _, row := range data.CurrentLiabilities {
			appendExcelRow(&buffer, []string{row.Name, formatExcelAmount(row.PriorAmount), formatExcelAmount(row.Amount)})
		}
		appendExcelRow(&buffer, []string{"TOTAL LIABILITIES", formatExcelAmount(data.PriorTotalLiabilities), formatExcelAmount(data.TotalLiabilities)})
		appendExcelRow(&buffer, []string{"NET ASSETS", formatExcelAmount(data.PriorTotalEquity), formatExcelAmount(data.TotalEquity)})
		buffer.WriteString(`</Table></Worksheet></Workbook>`)
		return buffer.Bytes(), fmt.Sprintf("ramseyer-balance-sheet-%d.xls", year), nil
	case "cash-flow":
		data, err := buildCashFlowData(year)
		if err != nil {
			return nil, "", err
		}
		var buffer bytes.Buffer
		writeExcelHeader(&buffer, "RAMSEYER PRESBYTERIAN CHURCH - AFIAMAN", "CASH FLOW REVIEW", year)
		appendExcelRow(&buffer, []string{"Description", "Amount (GHS)"})
		appendExcelRow(&buffer, []string{"Income entries", formatExcelAmount(data.IncomeEntries)})
		appendExcelRow(&buffer, []string{"Expenditure entries", formatExcelAmount(data.ExpenditureEntries)})
		appendExcelRow(&buffer, []string{"Income less expenditure", formatExcelAmount(data.IncomeLessExpenditure)})
		appendExcelRow(&buffer, []string{"PPE entries", formatExcelAmount(data.PPEEntries)})
		appendExcelRow(&buffer, []string{"Investment entries", formatExcelAmount(data.InvestmentEntries)})
		appendExcelRow(&buffer, []string{"Intangible asset entries", formatExcelAmount(data.IntangibleAssetEntries)})
		appendExcelRow(&buffer, []string{"All asset entries", formatExcelAmount(data.AssetEntries)})
		appendExcelRow(&buffer, []string{"All liability entries", formatExcelAmount(data.LiabilityEntries)})
		buffer.WriteString(`</Table></Worksheet></Workbook>`)
		return buffer.Bytes(), fmt.Sprintf("ramseyer-cash-flow-%d.xls", year), nil
	case "fixed-assets":
		data, err := buildFixedAssetData(year)
		if err != nil {
			return nil, "", err
		}
		var buffer bytes.Buffer
		writeExcelHeader(&buffer, "RAMSEYER PRESBYTERIAN CHURCH - AFIAMAN", "NOTE 21 - PROPERTY, PLANT & EQUIPMENT", year)
		appendExcelRow(&buffer, []string{"Asset class", "Additions", "Disposals", "Year total"})
		for _, row := range data.Lines {
			appendExcelRow(&buffer, []string{row.Name, formatExcelAmount(row.Additions), formatExcelAmount(row.Disposals), formatExcelAmount(row.YearTotal)})
		}
		appendExcelRow(&buffer, []string{"TOTAL", formatExcelAmount(data.TotalAdditions), formatExcelAmount(data.TotalDisposals), formatExcelAmount(data.TotalYear)})
		buffer.WriteString(`</Table></Worksheet></Workbook>`)
		return buffer.Bytes(), fmt.Sprintf("ramseyer-fixed-assets-%d.xls", year), nil
	case "notes":
		data, err := buildNotesData(year)
		if err != nil {
			return nil, "", err
		}
		var buffer bytes.Buffer
		writeExcelHeader(&buffer, "RAMSEYER PRESBYTERIAN CHURCH - AFIAMAN", "NOTES TO THE FINANCIAL STATEMENTS", year)
		appendExcelRow(&buffer, []string{"Note", "Description", fmt.Sprintf("%d", year-1), fmt.Sprintf("%d", year)})
		for _, note := range data.Notes {
			appendExcelRow(&buffer, []string{fmt.Sprintf("NOTE %s: %s", note.Number, strings.ToUpper(note.Title)), "", "", ""})
			for _, row := range note.Lines {
				appendExcelRow(&buffer, []string{"", row.Name, formatExcelAmount(row.PriorAmount), formatExcelAmount(row.Amount)})
			}
			appendExcelRow(&buffer, []string{"", "Total", formatExcelAmount(note.PriorTotal), formatExcelAmount(note.Total)})
		}
		buffer.WriteString(`</Table></Worksheet></Workbook>`)
		return buffer.Bytes(), fmt.Sprintf("ramseyer-notes-%d.xls", year), nil
	default:
		return nil, "", fmt.Errorf("unsupported report %q", report)
	}
}

func writeExcelHeader(buffer *bytes.Buffer, org, title string, year int) {
	buffer.WriteString(`<?xml version="1.0"?>`)
	buffer.WriteString(`
<Workbook xmlns="urn:schemas-microsoft-com:office:spreadsheet"
 xmlns:o="urn:schemas-microsoft-com:office:office"
 xmlns:x="urn:schemas-microsoft-com:office:excel"
 xmlns:ss="urn:schemas-microsoft-com:office:spreadsheet">
<Worksheet ss:Name="Report"><Table>
`)
	appendExcelRow(buffer, []string{org})
	appendExcelRow(buffer, []string{title})
	appendExcelRow(buffer, []string{"Reporting year: " + strconv.Itoa(year)})
	appendExcelRow(buffer, []string{})
}

func formatExcelAmount(value float64) string {
	return fmt.Sprintf("%.2f", value)
}
