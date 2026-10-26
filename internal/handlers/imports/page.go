package imports

import (
	"log"
	"net/http"
	"ramseyer-finance/internal/imports/importer"
	"ramseyer-finance/internal/webui"
)

// previewStore keeps parsed uploads between the preview and confirm requests. A package-level
// store matches the single-operator application and lets both handlers share the same TTL.
var previewStore = importer.NewPreviewStore()

// pageData carries the upload form and, after a preview, the complete review table.
type pageData struct {
	Active      string
	Message     string
	MessageTone string
	Year        string
	Preview     *importer.Preview
}

// Page serves the workbook import screen. The GET view only offers the upload form; the
// preview and confirmation stay in POST responses so a refresh cannot resubmit a workbook.
func Page(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	renderPage(w, pageData{Active: "import"})
}

func renderPage(w http.ResponseWriter, data pageData) {
	data.Active = "import"
	webui.RenderTemplate(w, "import", data)
}

func renderPageError(w http.ResponseWriter, message, year string) {
	renderPage(w, pageData{
		Message:     message,
		MessageTone: "warning",
		Year:        year,
	})
}

func renderPreview(w http.ResponseWriter, preview *importer.Preview) {
	renderPage(w, pageData{Preview: preview})
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("import request failed: %v", err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}
