package imports

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	workbookimport "ramseyer-finance/internal/imports/importer"
)

const (
	// maxUploadMegabytes is the largest workbook the preview will read. The real workbook is
	// under a megabyte, so this is generous while still bounding memory and parse time.
	maxUploadMegabytes = 20
	maxUploadBytes     = maxUploadMegabytes << 20
)

// PreviewUpload reads one workbook, parses it, and renders the review screen. Nothing is
// written to the register here; the parsed plan is staged and the operator must confirm it.
func PreviewUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		renderPageError(w, uploadFailureMessage(err), strings.TrimSpace(r.FormValue("year")))
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	yearValue := strings.TrimSpace(r.FormValue("year"))

	file, header, err := r.FormFile("workbook")
	if err != nil {
		renderPageError(w, "Choose an .xlsx workbook to preview.", yearValue)
		return
	}
	defer func() {
		_ = file.Close()
	}()

	if !strings.EqualFold(filepath.Ext(header.Filename), ".xlsx") {
		renderPageError(w, "Choose an .xlsx workbook. Other file types are not imported.", yearValue)
		return
	}
	if header.Size > int64(maxUploadBytes) {
		renderPageError(w, fmt.Sprintf("The workbook is larger than %d MB. Choose a smaller file.", maxUploadMegabytes), yearValue)
		return
	}

	data, err := io.ReadAll(io.LimitReader(file, int64(maxUploadBytes)+1))
	if err != nil {
		renderPageError(w, "The workbook could not be read from the upload. Try choosing it again.", yearValue)
		return
	}
	if len(data) > maxUploadBytes {
		renderPageError(w, fmt.Sprintf("The workbook is larger than %d MB. Choose a smaller file.", maxUploadMegabytes), yearValue)
		return
	}

	year := 0
	if yearValue != "" {
		parsed, err := strconv.Atoi(yearValue)
		if err != nil {
			renderPageError(w, "Enter a four-digit year or leave the year field blank to detect it from the workbook.", yearValue)
			return
		}
		year = parsed
	}

	preview, err := workbookimport.PreviewWorkbook(previewStore, header.Filename, data, year)
	if err != nil {
		if errors.Is(err, workbookimport.ErrUnreadableWorkbook) {
			detail := strings.TrimPrefix(err.Error(), workbookimport.ErrUnreadableWorkbook.Error())
			detail = strings.TrimPrefix(detail, ": ")
			renderPageError(w, "The workbook could not be read: "+detail, yearValue)
			return
		}
		serverError(w, err)
		return
	}

	renderPreview(w, preview)
}

func uploadFailureMessage(err error) string {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return fmt.Sprintf("The workbook is larger than %d MB. Choose a smaller file.", maxUploadMegabytes)
	}
	return "The upload could not be read. Choose one .xlsx workbook and try again."
}
