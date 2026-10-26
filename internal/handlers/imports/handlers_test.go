package imports

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"ramseyer-finance/internal/db"
	workbookimport "ramseyer-finance/internal/imports/importer"
	"ramseyer-finance/internal/imports/plan"
	"strconv"
	"strings"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) {
	t.Helper()

	if err := db.Initialize(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatalf("initialize test database: %v", err)
	}
	t.Cleanup(db.Close)
}

func TestPageRendersUploadForm(t *testing.T) {
	recorder := httptest.NewRecorder()
	Page(recorder, httptest.NewRequest(http.MethodGet, "/import", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Choose a workbook") {
		t.Fatal("upload form was not rendered")
	}
}

func TestPreviewTemplateRendersReportAndMapping(t *testing.T) {
	preview := &workbookimport.Preview{
		Token: "token",
		Plan: &plan.Plan{
			FileName: "SUNDAY OFFERTORY.xlsx",
			FileHash: "abc123",
			Year:     2026,
			Rows: []plan.Row{
				{Date: time.Date(2026, time.January, 4, 0, 0, 0, 0, time.UTC), Label: "Tithe", Key: "TITHES", Amount: 100},
			},
			Sheets: []plan.SheetReport{
				{Name: "08MAR", Kind: plan.KindWeekly, Rows: 1, Total: 100, Expected: 100, Reconciles: true},
				{Name: "MAY", Kind: plan.KindUndatedSummary, Note: "Undated summary. Not imported."},
			},
		},
		Categories: []workbookimport.CategoryOption{
			{ID: 1, Name: "Tithes", Label: "Tithes"},
		},
		Labels: []workbookimport.LabelView{
			{Key: "TITHES", Display: "Tithe", Rows: 1, Total: 100, Source: plan.CategoryStandard, CategoryID: 1, CategoryLabel: "Tithes"},
		},
		ExistingBatch: &workbookimport.BatchSummary{
			ID: 1, FileName: "old.xlsx", RowCount: 200, Total: 900, ImportedAt: "01 Jan 2026 10:00",
		},
		Warnings:   []string{"One warning."},
		Unresolved: map[string]bool{},
	}

	recorder := httptest.NewRecorder()
	renderPreview(recorder, preview)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, expected := range []string{"Sheet report", "Account mapping", "already has an imported batch", "Confirm Import", "08MAR"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("rendered preview is missing %q", expected)
		}
	}
}

func TestCommitImportReadsMappedAccount(t *testing.T) {
	setupTestDB(t)

	var titheID int64
	if err := db.DB.QueryRow(`SELECT id FROM categories WHERE type='income' AND name='Tithes'`).Scan(&titheID); err != nil {
		t.Fatalf("lookup Tithes: %v", err)
	}

	built := &plan.Plan{
		FileName: "SUNDAY OFFERTORY.xlsx",
		FileHash: "hash",
		Year:     2026,
		Rows: []plan.Row{
			{Date: time.Date(2026, time.January, 4, 0, 0, 0, 0, time.UTC), Label: "Tithe", Key: "TITHES", Amount: 100},
		},
		Labels: []plan.Label{
			{Key: "TITHES", Display: "Tithe", Rows: 1, Total: 100, Category: "Tithes", Source: plan.CategoryStandard},
		},
	}
	token, err := previewStore.Put(built)
	if err != nil {
		t.Fatalf("stage preview: %v", err)
	}

	form := url.Values{
		"token":           {token},
		"mapping[TITHES]": {strconv.FormatInt(titheID, 10)},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/import/commit", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	CommitImport(recorder, request)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); !strings.HasPrefix(location, "/transactions?year=2026&msg=") {
		t.Fatalf("location = %q", location)
	}

	var count int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM transactions`).Scan(&count); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if count != 1 {
		t.Fatalf("transactions = %d, want 1", count)
	}
}

func TestCommitImportExpiredPreviewRedirectsToUploadMessage(t *testing.T) {
	form := url.Values{"token": {"missing"}, "mapping[TITHES]": {"1"}}
	request := httptest.NewRequest(http.MethodPost, "/api/import/commit", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	CommitImport(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "This preview has expired") {
		t.Fatal("expired preview message was not rendered")
	}
}
