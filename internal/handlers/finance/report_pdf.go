package finance

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// ExportReportPDF builds a real PDF on the Go side instead of relying on window.print(),
// which is not consistently implemented by embedded WKWebView/WebView2 shells. Native
// delivery saves to Downloads and returns the path; browser delivery remains available for
// tests and future hosted use.
func ExportReportPDF(w http.ResponseWriter, r *http.Request) {
	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	report := strings.TrimSpace(r.URL.Query().Get("report"))
	title, lines, err := buildReportPDFLines(report, year)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	content, err := renderTextPDF(append([]string{
		"RAMSEYER PRESBYTERIAN CHURCH - AFIAMAN",
		title,
		"Reporting year: " + strconv.Itoa(year),
		"",
	}, lines...))
	if err != nil {
		serverError(w, err)
		return
	}
	filename := fmt.Sprintf("ramseyer-%s-%d.pdf", strings.ReplaceAll(report, "_", "-"), year)
	if r.URL.Query().Get("delivery") == "native" {
		saveExportLocally(w, filename, content)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func buildReportPDFLines(report string, year int) (string, []string, error) {
	switch report {
	case "trial-balance":
		data, err := buildTrialBalanceData(year)
		if err != nil {
			return "", nil, err
		}
		lines := []string{"Note  Type          Account                                                   Amount"}
		for _, row := range data.Lines {
			lines = append(lines, fmt.Sprintf("%-5s %-13s %-48s %14.2f", row.Note, row.AccountType, truncateForPDF(row.Account, 48), row.Amount))
		}
		lines = append(lines, "", pdfAmountLine("Income total", data.IncomeTotal), pdfAmountLine("Expenditure total", data.ExpenditureTotal), pdfAmountLine("Asset total", data.AssetTotal), pdfAmountLine("Liability total", data.LiabilityTotal))
		return "TRIAL BALANCE", lines, nil
	case "annual":
		data, err := buildAnnualData(year)
		if err != nil {
			return "", nil, err
		}
		lines := []string{"STATEMENT OF INCOME AND EXPENDITURE", "", "INCOME", "Note  Description                              Prior          Current"}
		for _, row := range data.IncomeLines {
			lines = append(lines, reportLine(row.Note, row.Name, row.PriorAmount, row.Amount))
		}
		lines = append(lines, "", "EXPENDITURE")
		for _, row := range data.ExpenseLines {
			lines = append(lines, reportLine(row.Note, row.Name, row.PriorAmount, row.Amount))
		}
		lines = append(lines, "", fmt.Sprintf("SURPLUS / (DEFICIT)%43.2f %14.2f", data.PriorSurplus, data.Surplus))
		return "ANNUAL FINANCIAL PERFORMANCE", lines, nil
	case "balance-sheet":
		data, err := buildBalanceData(year)
		if err != nil {
			return "", nil, err
		}
		lines := []string{"STATEMENT OF FINANCIAL POSITION", "", "Description                                      Prior          Current", "NON-CURRENT ASSETS"}
		for _, row := range data.NonCurrentAssets {
			lines = append(lines, reportLine("", row.Name, row.PriorAmount, row.Amount))
		}
		lines = append(lines, "", "CURRENT ASSETS")
		for _, row := range data.CurrentAssets {
			lines = append(lines, reportLine("", row.Name, row.PriorAmount, row.Amount))
		}
		lines = append(lines, fmt.Sprintf("TOTAL ASSETS%48.2f %14.2f", data.PriorTotalAssets, data.TotalAssets), "", "LIABILITIES")
		for _, row := range data.LongTermLiabilities {
			lines = append(lines, reportLine("", row.Name, row.PriorAmount, row.Amount))
		}
		for _, row := range data.CurrentLiabilities {
			lines = append(lines, reportLine("", row.Name, row.PriorAmount, row.Amount))
		}
		lines = append(lines, fmt.Sprintf("TOTAL LIABILITIES%43.2f %14.2f", data.PriorTotalLiabilities, data.TotalLiabilities), fmt.Sprintf("NET ASSETS%50.2f %14.2f", data.PriorTotalEquity, data.TotalEquity))
		return "STATEMENT OF FINANCIAL POSITION", lines, nil
	case "cash-flow":
		data, err := buildCashFlowData(year)
		if err != nil {
			return "", nil, err
		}
		lines := []string{
			"CASH FLOW REVIEW", "",
			pdfAmountLine("Income entries", data.IncomeEntries), pdfAmountLine("Expenditure entries", data.ExpenditureEntries), pdfAmountLine("Income less expenditure", data.IncomeLessExpenditure), "",
			pdfAmountLine("PPE entries", data.PPEEntries), pdfAmountLine("Investment entries", data.InvestmentEntries), pdfAmountLine("Intangible asset entries", data.IntangibleAssetEntries), pdfAmountLine("All asset entries", data.AssetEntries), "",
			pdfAmountLine("All liability entries", data.LiabilityEntries),
		}
		return "CASH FLOW REVIEW", lines, nil
	case "fixed-assets":
		data, err := buildFixedAssetData(year)
		if err != nil {
			return "", nil, err
		}
		lines := []string{"NOTE 21 - PROPERTY, PLANT & EQUIPMENT", "", "Asset class                              Additions     Disposals    Year total"}
		for _, row := range data.Lines {
			lines = append(lines, fmt.Sprintf("%-39s %12.2f %12.2f %13.2f", truncateForPDF(row.Name, 39), row.Additions, row.Disposals, row.YearTotal))
		}
		lines = append(lines, fmt.Sprintf("TOTAL%46.2f %12.2f %13.2f", data.TotalAdditions, data.TotalDisposals, data.TotalYear))
		return "NOTE 21 - PROPERTY, PLANT & EQUIPMENT", lines, nil
	case "notes":
		data, err := buildNotesData(year)
		if err != nil {
			return "", nil, err
		}
		lines := []string{"NOTES TO THE FINANCIAL STATEMENTS", "", "Description                                      Prior          Current"}
		for _, note := range data.Notes {
			lines = append(lines, "", fmt.Sprintf("NOTE %s: %s", note.Number, strings.ToUpper(note.Title)))
			for _, row := range note.Lines {
				lines = append(lines, reportLine("", row.Name, row.PriorAmount, row.Amount))
			}
			lines = append(lines, reportLine("", "Total", note.PriorTotal, note.Total))
		}
		return "NOTES TO THE FINANCIAL STATEMENTS", lines, nil
	default:
		return "", nil, fmt.Errorf("unsupported report %q", report)
	}
}

func reportLine(note, name string, prior, current float64) string {
	return fmt.Sprintf("%-5s %-38s %14.2f %14.2f", note, truncateForPDF(name, 38), prior, current)
}

func pdfAmountLine(name string, amount float64) string {
	return fmt.Sprintf("%-60s %14.2f", truncateForPDF(name, 60), amount)
}
