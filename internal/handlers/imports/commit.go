package imports

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	workbookimport "ramseyer-finance/internal/imports/importer"
)

// maxCommitFormBytes bounds the confirmation form. The form only carries a token and one
// account ID per label, so a megabyte leaves room for a large workbook's label list without
// accepting an arbitrary body.
const maxCommitFormBytes = 1 << 20

// CommitImport validates the reviewed mapping and writes the batch. A validation failure
// redraws the same preview with the operator's valid choices intact; a storage failure is
// returned as a server error because the register was not changed.
func CommitImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCommitFormBytes)
	if err := r.ParseForm(); err != nil {
		renderPageError(w, "The confirmation form could not be read. Preview the workbook again.", "")
		return
	}

	token := r.FormValue("token")
	if token == "" {
		renderPageError(w, "Upload a workbook and preview it before confirming the import.", "")
		return
	}

	built, exists := previewStore.Get(token)
	if !exists {
		renderPageError(w, "This preview has expired. Upload the workbook again to review it before saving.", "")
		return
	}

	submitted := make(map[string]string, len(built.Labels))
	for _, label := range built.Labels {
		submitted[label.Key] = r.FormValue("mapping[" + label.Key + "]")
	}

	result, err := workbookimport.Commit(previewStore, token, built, submitted)
	if err != nil {
		var mappingErr *workbookimport.MappingError
		if errors.As(err, &mappingErr) {
			preview, previewErr := workbookimport.PreviewSelections(token, built, mappingErr.Selected)
			if previewErr != nil {
				serverError(w, previewErr)
				return
			}
			preview.Message = mappingErr.Error()
			preview.MessageTone = "warning"
			preview.Unresolved = mappingErr.UnresolvedKeys
			renderPreview(w, preview)
			return
		}
		if errors.Is(err, workbookimport.ErrNothingToImport) {
			preview, previewErr := workbookimport.PreviewSelections(token, built, map[string]int64{})
			if previewErr != nil {
				serverError(w, previewErr)
				return
			}
			preview.Message = err.Error()
			preview.MessageTone = "warning"
			renderPreview(w, preview)
			return
		}
		serverError(w, err)
		return
	}

	message := fmt.Sprintf("Imported %d rows totalling %.2f for %d.", result.RowCount, result.Total, result.Year)
	if result.ReplacedRows > 0 {
		message += fmt.Sprintf(" Replaced the previous batch of %d rows.", result.ReplacedRows)
	}
	http.Redirect(w, r, fmt.Sprintf("/transactions?year=%d&msg=%s", result.Year, url.QueryEscape(message)), http.StatusSeeOther)
}
