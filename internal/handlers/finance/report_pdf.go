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
		lines := []string{"Note  Type          Account                                  Debit          Credit"}
		for _, row := range data.Lines {
			lines = append(lines, fmt.Sprintf("%-5s %-13s %-36s %12.2f %12.2f", row.Note, row.AccountType, truncateForPDF(row.Account, 36), row.Debit, row.Credit))
		}
		lines = append(lines, fmt.Sprintf("TOTAL%61.2f %12.2f", data.TotalDebit, data.TotalCredit), fmt.Sprintf("DIFFERENCE: %.2f", data.Difference))
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
		lines = append(lines, fmt.Sprintf("TOTAL LIABILITIES%43.2f %14.2f", data.PriorTotalLiabilities, data.TotalLiabilities), fmt.Sprintf("TOTAL EQUITY%48.2f %14.2f", data.PriorTotalEquity, data.TotalEquity), fmt.Sprintf("BALANCE DIFFERENCE%42.2f %14.2f", data.PriorBalanceDifference, data.BalanceDifference))
		return "STATEMENT OF FINANCIAL POSITION", lines, nil
	case "cash-flow":
		data, err := buildCashFlowData(year)
		if err != nil {
			return "", nil, err
		}
		lines := []string{
			"STATEMENT OF CASH FLOWS", "",
			pdfAmountLine("Surplus / (Deficit)", data.Surplus), pdfAmountLine("Depreciation & amortization", data.DepreciationAmortization), pdfAmountLine("Changes in inventory", data.InventoryMovement), pdfAmountLine("Changes in receivables", data.ReceivablesMovement), pdfAmountLine("Changes in payables", data.PayablesMovement), pdfAmountLine("Net operating cash", data.NetOperatingCash), "",
			pdfAmountLine("PPE acquisitions", -data.PPEAcquisitions), pdfAmountLine("Investment acquisitions", -data.InvestmentAcquisitions), pdfAmountLine("Intangible acquisitions", -data.IntangibleAcquisitions), pdfAmountLine("Net investing cash", data.NetInvestingCash), "",
			pdfAmountLine("Long-term loan movement", data.LongTermLoanMovement), pdfAmountLine("Net financing cash", data.NetFinancingCash), "", pdfAmountLine("Opening cash", data.OpeningCash), pdfAmountLine("Net cash change", data.NetCashChange), pdfAmountLine("Reported closing cash", data.ReportedClosingCash), pdfAmountLine("Reconciliation difference", data.ReconciliationDifference),
		}
		return "STATEMENT OF CASH FLOWS", lines, nil
	case "fixed-assets":
		data, err := buildFixedAssetData(year)
		if err != nil {
			return "", nil, err
		}
		lines := []string{"NOTE 21 - NON-CURRENT ASSETS SCHEDULE", "", "Asset class                         Open cost    Additions   Closing cost      Carrying"}
		for _, row := range data.Lines {
			lines = append(lines, fmt.Sprintf("%-34s %11.2f %11.2f %14.2f %13.2f", truncateForPDF(row.Name, 34), row.OpeningCost, row.Additions, row.ClosingCost, row.CarryingAmount))
		}
		lines = append(lines, fmt.Sprintf("TOTAL%41.2f %11.2f %14.2f %13.2f", data.TotalOpeningCost, data.TotalAdditions, data.TotalClosingCost, data.TotalCarryingAmount))
		return "NOTE 21 - NON-CURRENT ASSETS", lines, nil
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
