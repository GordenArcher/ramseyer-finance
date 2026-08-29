package finance

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"ramseyer-finance/internal/db"
	backupservice "ramseyer-finance/internal/handlers/backup"
	"strconv"
	"strings"
	"time"
)

// ExportTransactionRow represents a single transaction row as it appears in an exported
// file (CSV, Excel, or PDF). Unlike the internal transaction model, this struct includes
// the resolved top-level category name and the note reference from the categories table,
// joined at query time so the export file is self-contained and readable without access
// to the original database. The ID and CreatedAt fields are included for audit trail
// purposes, and the Amount is carried as a float64 for straightforward numeric formatting
// in the export builders.
type ExportTransactionRow struct {
	ID            int64
	Date          string
	Type          string
	TopCategory   string
	Category      string
	PaymentMethod string
	NoteRef       string
	Description   string
	Amount        float64
	CreatedAt     string
}

// pdfObject represents a single indirect object in a hand-crafted PDF document. Each
// object has a unique integer ID (used for cross-reference entries) and a body string
// containing the object's dictionary and optional stream data. This minimal model avoids
// pulling in a full PDF generation library for what is essentially a fixed-width text
// report printed to a PDF canvas.
type pdfObject struct {
	ID   int
	Body string
}

// ExportTransactions handles GET requests for transaction data export in CSV, Excel
// (SpreadsheetML), or PDF format. It parses the date range (inclusive start, inclusive
// end), an optional transaction type filter, and the desired output format from query
// parameters, loads the matching transactions from the database with resolved category
// metadata, builds the export file in the requested format, and delivers it either as a
// browser download (with Content-Disposition attachment headers) or as a native filesystem
// save (for the desktop webview, returning a JSON response with the saved path).
// The function is intentionally a GET endpoint so that export requests are bookmarkable
// and easy to retry from the register page's filter UI.
func ExportTransactions(w http.ResponseWriter, r *http.Request) {
	// I keep exports as GET requests because they are read-only and naturally parameterized by
	// query filters. That makes them easy to retry, share, and bookmark from the register view.
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// I validate the export format explicitly because response type, filename, and generation path
	// branch significantly by format.
	format := strings.TrimSpace(r.URL.Query().Get("format"))
	switch format {
	case "csv", "excel", "pdf":
	default:
		badRequest(w, "Invalid export format")
		return
	}

	// Parse the inclusive date range from the query string and convert the end date into an
	// exclusive upper bound for SQL querying. This validation happens before any database
	// work so that malformed dates fail fast with a clear error message.
	startDate, endDate, endExclusive, err := parseExportDateRange(
		r.URL.Query().Get("start_date"),
		r.URL.Query().Get("end_date"),
	)
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	// Validate the transaction type filter against the four known types. An empty string
	// means "all types," which is the most common export use case.
	filterType := strings.TrimSpace(r.URL.Query().Get("type"))
	switch filterType {
	case "", "income", "expenditure", "asset", "liability":
	default:
		badRequest(w, "Invalid transaction type filter")
		return
	}

	rows, err := loadExportTransactions(startDate, endExclusive, filterType)
	if err != nil {
		serverError(w, err)
		return
	}

	// Build a descriptive base filename that includes the date range and optional type
	// filter so the downloaded file is immediately identifiable without opening it.
	filenameBase := fmt.Sprintf(
		"ramseyer-transactions-%s-to-%s",
		startDate,
		endDate,
	)
	if filterType != "" {
		filenameBase += "-" + filterType
	}

	filename, contentType, content, err := buildTransactionExportFile(
		format,
		rows,
		filenameBase,
		startDate,
		endDate,
		filterType,
	)
	if err != nil {
		serverError(w, err)
		return
	}

	// I support native delivery separately because the desktop shell wants a saved-file result
	// rather than a browser-style attachment download.
	if strings.TrimSpace(r.URL.Query().Get("delivery")) == "native" {
		saveExportLocally(w, filename, content)
		return
	}

	// Standard browser download: set the Content-Type to match the file format and
	// Content-Disposition to "attachment" so the browser prompts the user to save the
	// file rather than attempting to display it inline.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	_, _ = w.Write(content)
}

// parseExportDateRange validates and normalises the start and end date strings from the
// export request. It accepts dates in "2006-01-02" format (the standard across the app),
// ensures the end date is not before the start date, and returns the original inclusive
// dates alongside an exclusive end date (endDate + 1 day) that can be used directly in
// SQL queries with a "<" comparison. This centralises the inclusive-to-exclusive
// conversion so every export query uses the same index-friendly range predicate.
func parseExportDateRange(startRaw, endRaw string) (string, string, string, error) {
	// I convert the inclusive end date into an exclusive upper bound once here so every export
	// query can use the same index-friendly range predicate.
	startDate, err := parseTransactionDate(startRaw)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid start date")
	}
	endDate, err := parseTransactionDate(endRaw)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid end date")
	}

	startTime, _ := time.Parse("2006-01-02", startDate)
	endTime, _ := time.Parse("2006-01-02", endDate)
	if endTime.Before(startTime) {
		return "", "", "", fmt.Errorf("end date must be on or after start date")
	}

	// Return the inclusive dates for display and filename purposes, and the exclusive
	// end date for SQL WHERE clauses (t.date >= startDate AND t.date < endExclusive).
	return startDate, endDate, endTime.AddDate(0, 0, 1).Format("2006-01-02"), nil
}

// loadExportTransactions queries the transactions table for all rows within the given date
// range and optional type filter, joining against the categories table to resolve both the
// direct category and its top-level parent. This means an exported transaction posted to
// "Children Service" (a subcategory of "Offering") will show "Offering" as the top category
// and "Children Service" as the category, making the export file readable even for users
// unfamiliar with the category hierarchy. Results are ordered by date and then by ID so
// the export is chronologically sorted and deterministic.
func loadExportTransactions(startDate, endExclusive, filterType string) ([]ExportTransactionRow, error) {
	// I join category and parent metadata during export so the saved file remains readable even
	// when the underlying posting was made to a subcategory.
	args := []interface{}{startDate, endExclusive}
	query := strings.Builder{}
	query.WriteString(`
		SELECT
			t.id,
			t.date,
			t.type,
			COALESCE(parent.name, category.name, t.category),
			COALESCE(category.name, t.category),
			COALESCE(NULLIF(t.payment_method, ''), 'cash'),
			COALESCE(category.note_ref, t.note_ref, ''),
			COALESCE(t.description, ''),
			t.amount,
			COALESCE(t.created_at, '')
		FROM transactions t
		LEFT JOIN categories category ON category.id = t.category_id
		LEFT JOIN categories parent ON parent.id = category.parent_id
		WHERE t.date >= ? AND t.date < ?
	`)
	// Append the type filter only when one is specified. Using a parameterised query
	// rather than string interpolation prevents SQL injection even though the filter
	// value is validated against a whitelist earlier in the request flow.
	if filterType != "" {
		query.WriteString(` AND t.type = ?`)
		args = append(args, filterType)
	}
	query.WriteString(` ORDER BY t.date ASC, t.id ASC`)

	rows, err := db.DB.Query(query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query export transactions: %w", err)
	}
	defer rows.Close()

	var transactions []ExportTransactionRow
	for rows.Next() {
		var row ExportTransactionRow
		if err := rows.Scan(
			&row.ID,
			&row.Date,
			&row.Type,
			&row.TopCategory,
			&row.Category,
			&row.PaymentMethod,
			&row.NoteRef,
			&row.Description,
			&row.Amount,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan export transaction: %w", err)
		}
		row.PaymentMethod = paymentMethodLabel(row.PaymentMethod)
		transactions = append(transactions, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate export transactions: %w", err)
	}

	return transactions, nil
}

// buildTransactionsCSV encodes the export rows as a standard RFC 4180 CSV file with a
// header row. The CSV format is deliberately plain and tabular—no metadata headers, no
// summary rows—because its primary use case is importing into spreadsheet applications
// or other accounting tools that expect a clean rectangular data grid. Amounts are
// formatted to two decimal places, and all fields are written as strings to preserve
// leading zeros in dates and note references.
func buildTransactionsCSV(rows []ExportTransactionRow) ([]byte, error) {
	// I keep CSV strictly tabular and plain because its main job is interoperability, not layout.
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"ID", "Date", "Type", "Top Category", "Category", "Payment Method", "Note", "Description", "Amount", "Created At"})
	for _, row := range rows {
		_ = writer.Write([]string{
			strconv.FormatInt(row.ID, 10),
			row.Date,
			row.Type,
			row.TopCategory,
			row.Category,
			row.PaymentMethod,
			row.NoteRef,
			row.Description,
			fmt.Sprintf("%.2f", row.Amount),
			row.CreatedAt,
		})
	}
	// Flush writes any buffered data to the underlying buffer and returns any error that
	// occurred during the write. We check this after the loop rather than on each Write
	// call because csv.Writer buffers and only reports errors at Flush time.
	writer.Flush()
	return buffer.Bytes(), writer.Error()
}

// buildTransactionsExcel generates a SpreadsheetML (XML-based Excel 2003) file containing
// the exported transactions. SpreadsheetML is chosen over a modern XLSX format to avoid
// pulling in a ZIP and OpenXML dependency—the export needs are simple (a single worksheet
// with a header and data rows), and SpreadsheetML produces a valid .xls file that every
// major spreadsheet application can open. The output includes metadata rows at the top
// (report title, date range, type filter) followed by the column headers and data.
func buildTransactionsExcel(rows []ExportTransactionRow, startDate, endDate, filterType string) []byte {
	// I generate SpreadsheetML instead of a heavier XLSX dependency because the export needs are
	// simple and this keeps the binary smaller and easier to maintain.
	var buffer bytes.Buffer
	buffer.WriteString(`<?xml version="1.0"?>`)
	buffer.WriteString(`
<Workbook xmlns="urn:schemas-microsoft-com:office:spreadsheet"
 xmlns:o="urn:schemas-microsoft-com:office:office"
 xmlns:x="urn:schemas-microsoft-com:office:excel"
 xmlns:ss="urn:schemas-microsoft-com:office:spreadsheet">
<Worksheet ss:Name="Transactions"><Table>
`)
	// Metadata header rows give context to the exported data so the recipient knows
	// exactly what date range and filter produced this file, even without the filename.
	appendExcelRow(&buffer, []string{
		"Ramseyer Finance Transaction Export",
	})
	appendExcelRow(&buffer, []string{
		"Date Range", startDate + " to " + endDate,
	})
	appendExcelRow(&buffer, []string{
		"Type Filter", exportFilterLabel(filterType),
	})
	appendExcelRow(&buffer, []string{})
	appendExcelRow(&buffer, []string{"ID", "Date", "Type", "Top Category", "Category", "Payment Method", "Note", "Description", "Amount", "Created At"})

	for _, row := range rows {
		appendExcelRow(&buffer, []string{
			strconv.FormatInt(row.ID, 10),
			row.Date,
			row.Type,
			row.TopCategory,
			row.Category,
			row.PaymentMethod,
			row.NoteRef,
			row.Description,
			fmt.Sprintf("%.2f", row.Amount),
			row.CreatedAt,
		})
	}

	buffer.WriteString(`</Table></Worksheet></Workbook>`)
	return buffer.Bytes()
}

// appendExcelRow writes a single SpreadsheetML <Row> element containing one <Cell> per
// value. All cells are typed as String to avoid Excel auto-formatting issues (e.g.,
// treating "001" as the number 1 or interpreting dates in unexpected formats). Values
// are XML-escaped before embedding to handle characters like &, <, >, and quotes that
// would otherwise break the XML structure.
func appendExcelRow(buffer *bytes.Buffer, values []string) {
	buffer.WriteString("<Row>")
	for _, value := range values {
		buffer.WriteString(`<Cell><Data ss:Type="String">`)
		buffer.WriteString(xmlEscape(value))
		buffer.WriteString(`</Data></Cell>`)
	}
	buffer.WriteString("</Row>")
}

// xmlEscape replaces the five special XML characters with their corresponding entity
// references. This is the minimum escaping needed to produce well-formed XML when
// embedding arbitrary user-generated text (category names, descriptions, etc.) into
// SpreadsheetML markup.
func xmlEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(value)
}

// buildTransactionsPDF generates a PDF document containing the exported transactions as a
// fixed-width text report. The output is designed for printable archival purposes rather
// than visual polish: it uses a monospaced Courier font, fixed column widths, and
// automatic page breaking. The report includes a title header, date range and filter
// metadata, a column header line, the transaction rows, a row count, and a total amount.
// Text values longer than their column width are truncated with an ellipsis character.
func buildTransactionsPDF(rows []ExportTransactionRow, startDate, endDate, filterType string) ([]byte, error) {
	// I render PDF as a fixed-width text report because the real requirement is printable archival
	// output, not a complex visual layout engine.
	lines := []string{
		"Ramseyer Finance Transaction Export",
		fmt.Sprintf("Date Range: %s to %s", startDate, endDate),
		fmt.Sprintf("Type Filter: %s", exportFilterLabel(filterType)),
		"",
		"Date       Type       Category                Method     Note Description              Amount",
		"----------------------------------------------------------------------------------------------------",
	}

	// Build each data line with fixed-width formatting. Categories that belong to a
	// parent are displayed as "Parent/Category" so the hierarchy is visible in the
	// flat text output. Each column is truncated to its allocated width to maintain
	// alignment across all rows.
	var total float64
	for _, row := range rows {
		total += row.Amount
		category := row.Category
		if row.TopCategory != "" && row.TopCategory != row.Category {
			category = row.TopCategory + "/" + row.Category
		}
		lines = append(lines, fmt.Sprintf(
			"%-10s %-10s %-23s %-10s %-4s %-24s %12.2f",
			row.Date,
			truncateForPDF(row.Type, 10),
			truncateForPDF(category, 23),
			truncateForPDF(row.PaymentMethod, 10),
			truncateForPDF(row.NoteRef, 4),
			truncateForPDF(row.Description, 24),
			row.Amount,
		))
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Total Rows: %d", len(rows)))
	lines = append(lines, fmt.Sprintf("Total Amount: %.2f", total))

	return renderTextPDF(lines)
}

// renderTextPDF takes a slice of text lines and produces a complete, valid PDF 1.4
// document. It handles pagination automatically, splitting the lines into pages of up to
// 42 lines each (suitable for A4 landscape with 9pt Courier at 12pt line spacing). Each
// page includes a footer with "Page X of Y". The PDF is constructed manually using
// PDF's native text operators (BT/ET, Tf, Td, TL, Tj, T*) rather than a higher-level
// library, keeping the dependency footprint minimal. The document structure follows the
// classic PDF cross-reference table format with indirect objects for the catalog, pages
// tree, font resource, and page content streams.
func renderTextPDF(lines []string) ([]byte, error) {
	const (
		pageWidth    = 842
		pageHeight   = 595
		marginLeft   = 36
		marginTop    = 560
		lineHeight   = 12
		linesPerPage = 42
		fontObjectID = 3
		firstPageID  = 4
	)

	// Split the flat list of lines into pages. Each page gets up to linesPerPage lines;
	// the remainder spill onto subsequent pages. An empty result set still produces a
	// single page with a "No transactions found" message.
	var pages [][]string
	for start := 0; start < len(lines); start += linesPerPage {
		end := start + linesPerPage
		if end > len(lines) {
			end = len(lines)
		}
		pages = append(pages, lines[start:end])
	}
	if len(pages) == 0 {
		pages = append(pages, []string{"No transactions found for the selected period."})
	}

	// Build the document's indirect objects: the catalog (object 1), the pages tree
	// (object 2), the font resource (object 3), and then pairs of page dictionary +
	// content stream objects for each page, starting from firstPageID (4).
	objects := []pdfObject{
		{ID: 1, Body: "<< /Type /Catalog /Pages 2 0 R >>"},
		{ID: 2, Body: buildPagesObject(firstPageID, len(pages))},
		{ID: fontObjectID, Body: "<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>"},
	}

	nextID := firstPageID
	for index, pageLines := range pages {
		pageID := nextID
		contentID := nextID + 1
		nextID += 2

		// Each page dictionary references the shared pages tree as its parent, defines
		// the media box (page dimensions in PDF points, 1/72 inch), the font resource,
		// and the content stream that contains the actual text drawing commands.
		objects = append(objects, pdfObject{
			ID: pageID,
			Body: fmt.Sprintf(
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>",
				pageWidth,
				pageHeight,
				fontObjectID,
				contentID,
			),
		})

		stream := buildPDFPageStream(pageLines, index+1, len(pages), marginLeft, marginTop, lineHeight)
		objects = append(objects, pdfObject{
			ID:   contentID,
			Body: fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		})
	}

	return buildPDFDocument(objects)
}

// buildPagesObject generates the PDF pages tree dictionary, which lists all page objects
// in the document as an array of indirect references. This is referenced by the catalog
// object and is required for any PDF viewer to locate and render the pages.
func buildPagesObject(firstPageID, pageCount int) string {
	var kids []string
	for index := 0; index < pageCount; index++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", firstPageID+(index*2)))
	}
	return fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pageCount)
}

// buildPDFPageStream generates the content stream for a single PDF page. It sets up the
// text state (Courier 9pt, positioned at the top-left margin with the specified line
// height), draws each line of text using the Tj (show text) and T* (next line) operators,
// and appends a page number footer. PDF text strings are escaped to handle backslashes
// and parentheses, which have special meaning in PDF string literals.
func buildPDFPageStream(lines []string, pageNumber, totalPages, marginLeft, marginTop, lineHeight int) string {
	var buffer bytes.Buffer
	buffer.WriteString("BT\n")
	buffer.WriteString("/F1 9 Tf\n")
	buffer.WriteString(fmt.Sprintf("%d %d Td\n", marginLeft, marginTop))
	buffer.WriteString(fmt.Sprintf("%d TL\n", lineHeight))
	for _, line := range lines {
		buffer.WriteString(fmt.Sprintf("(%s) Tj\nT*\n", escapePDFText(line)))
	}
	// The page footer is drawn after all data lines. It uses the same T* operator to
	// advance to the next line, placing it just below the last line of content.
	buffer.WriteString(fmt.Sprintf("(Page %d of %d) Tj\n", pageNumber, totalPages))
	buffer.WriteString("ET")
	return buffer.String()
}

// buildPDFDocument serialises the complete PDF file structure: a header with the PDF
// version, all indirect objects with their byte offsets recorded, a cross-reference table
// mapping object numbers to file offsets, and a trailer dictionary pointing to the root
// catalog object. The cross-reference table is required for random-access reading by PDF
// viewers, even though this document is typically consumed sequentially.
func buildPDFDocument(objects []pdfObject) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString("%PDF-1.4\n")

	// Write each indirect object and record its byte offset for the cross-reference
	// table that follows. The offsets map is keyed by object ID.
	offsets := map[int]int{}
	for _, object := range objects {
		offsets[object.ID] = buffer.Len()
		buffer.WriteString(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", object.ID, object.Body))
	}

	xrefOffset := buffer.Len()
	maxID := 0
	for _, object := range objects {
		if object.ID > maxID {
			maxID = object.ID
		}
	}

	// The cross-reference table starts with a free entry (object 0, generation 65535)
	// followed by one entry per object. Each entry is a 10-digit byte offset, a 5-digit
	// generation number, and an "n" (in-use) or "f" (free) flag.
	buffer.WriteString(fmt.Sprintf("xref\n0 %d\n", maxID+1))
	buffer.WriteString("0000000000 65535 f \n")
	for id := 1; id <= maxID; id++ {
		buffer.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[id]))
	}
	buffer.WriteString(fmt.Sprintf(
		"trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF",
		maxID+1,
		xrefOffset,
	))

	return buffer.Bytes(), nil
}

// escapePDFText escapes backslashes and parentheses in a string so it can be safely
// embedded inside a PDF string literal (parenthesised string). Backslashes become double
// backslashes, and parentheses become escaped with a backslash prefix. These are the only
// characters that have special meaning inside PDF string literals.
func escapePDFText(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`(`, `\(`,
		`)`, `\)`,
	)
	return replacer.Replace(value)
}

// truncateForPDF shortens a string to fit within a fixed character width for PDF column
// alignment. If the string is already within the limit, it is returned unchanged. If the
// string exceeds the limit, it is truncated to (width - 1) runes and suffixed with an
// ellipsis character (…) to indicate truncation. The function operates on runes rather
// than bytes to handle multi-byte characters correctly, though the PDF uses Courier
// (a monospaced font) where most characters occupy equal width.
func truncateForPDF(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= width {
		return string(runes)
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

// exportFilterLabel converts a filter type string into a human-readable label for display
// in export file headers. An empty string (meaning "all types") becomes "All Transactions";
// specific types have their first letter capitalised (e.g., "income" becomes "Income").
func exportFilterLabel(filterType string) string {
	if filterType == "" {
		return "All Transactions"
	}
	return strings.ToUpper(filterType[:1]) + filterType[1:]
}

// buildTransactionExportFile dispatches to the appropriate format-specific builder based
// on the requested format string. It returns the complete filename (with extension), the
// MIME content type for HTTP response headers, and the file content as a byte slice.
// This function centralises the format-to-builder mapping so the main handler doesn't
// need a switch statement for each format combination.
func buildTransactionExportFile(
	format string,
	rows []ExportTransactionRow,
	filenameBase, startDate, endDate, filterType string,
) (string, string, []byte, error) {
	switch format {
	case "csv":
		content, err := buildTransactionsCSV(rows)
		if err != nil {
			return "", "", nil, err
		}
		return filenameBase + ".csv", "text/csv; charset=utf-8", content, nil
	case "excel":
		return filenameBase + ".xls", "application/vnd.ms-excel", buildTransactionsExcel(rows, startDate, endDate, filterType), nil
	case "pdf":
		content, err := buildTransactionsPDF(rows, startDate, endDate, filterType)
		if err != nil {
			return "", "", nil, err
		}
		return filenameBase + ".pdf", "application/pdf", content, nil
	default:
		return "", "", nil, fmt.Errorf("unsupported export format %q", format)
	}
}

// saveExportLocally writes the export file directly to the user's downloads directory and
// returns a JSON response with the saved path and filename. This is the "native" delivery
// path for the desktop webview environment, where Content-Disposition attachment headers
// may not trigger a typical browser download dialog. If the target filename already
// exists, nextAvailableFilePath appends a numeric suffix to avoid overwriting previous
// exports. The file is written with 0644 permissions (owner read/write, group and others
// read-only).
func saveExportLocally(w http.ResponseWriter, filename string, content []byte) {
	// I save exports locally on behalf of the desktop shell because an embedded webview does not
	// guarantee the same download UX as a full browser.
	downloadsDir, err := backupservice.ResolveUserDownloadsDir()
	if err != nil {
		serverError(w, err)
		return
	}

	targetPath := backupservice.NextAvailableFilePath(downloadsDir, filename)
	if err := os.WriteFile(targetPath, content, 0o644); err != nil {
		serverError(w, fmt.Errorf("save export to downloads: %w", err))
		return
	}

	backupservice.WriteJSON(w, http.StatusOK, backupservice.SavedFileResponse{
		Path:     targetPath,
		Filename: filepath.Base(targetPath),
		Message:  "Export saved",
	})
}
