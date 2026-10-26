package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"ramseyer-finance/internal/db"
	"ramseyer-finance/internal/imports/plan"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestPreviewWorkbookHashesUploadAndAppliesSavedMapping(t *testing.T) {
	setupTestDB(t)

	savedCategory := incomeCategory(t, "Adult Service Offertory")
	if err := db.SetSetting(labelMapSetting, fmt.Sprintf(`{"TITHES":%d}`, savedCategory.ID)); err != nil {
		t.Fatalf("save label map: %v", err)
	}

	data := weeklyWorkbook(t, "08MAR", [][]string{
		{"", "1ST", "2ND", "TOTAL"},
		{"Tithe", "100", "", "100"},
		{"TOTAL", "100", "", "100"},
	})
	store := NewPreviewStore()

	preview, err := PreviewWorkbook(store, "/tmp/SUNDAY OFFERTORY.xlsx", data, 2026)
	if err != nil {
		t.Fatalf("PreviewWorkbook: %v", err)
	}

	sum := sha256.Sum256(data)
	if preview.Plan.FileHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("file hash = %q, want the SHA-256 of the upload", preview.Plan.FileHash)
	}
	if preview.Plan.FileName != "SUNDAY OFFERTORY.xlsx" {
		t.Fatalf("file name = %q, want the base name", preview.Plan.FileName)
	}
	if preview.Plan.Year != 2026 {
		t.Fatalf("year = %d, want 2026", preview.Plan.Year)
	}
	if preview.Token == "" {
		t.Fatal("preview token should not be empty")
	}
	if preview.ExistingBatch != nil {
		t.Fatalf("existing batch = %v, want none", preview.ExistingBatch)
	}
	if len(preview.Labels) != 1 {
		t.Fatalf("labels = %d, want 1", len(preview.Labels))
	}
	if preview.Labels[0].CategoryID != savedCategory.ID {
		t.Fatalf("label category = %d, want the saved choice %d", preview.Labels[0].CategoryID, savedCategory.ID)
	}
	if len(preview.Unresolved) != 0 {
		t.Fatalf("unresolved = %v, want none", preview.Unresolved)
	}
	if _, ok := store.Get(preview.Token); !ok {
		t.Fatal("preview should remain staged for the confirm request")
	}
}

func TestNewPreviewMarksLabelsWithoutAnAccount(t *testing.T) {
	built := &plan.Plan{
		Labels: []plan.Label{
			{Key: "TITHES", Display: "Tithe", Rows: 2, Total: 150, Category: "Tithes", Source: plan.CategoryStandard},
			{Key: "DAY BORN", Display: "Day Born", Rows: 1, Total: 50, Source: plan.CategoryUndecided},
		},
	}
	categories := testCategories()

	preview := newPreview("token", built, categories, map[string]int64{"TITHES": 1}, nil)
	if preview.Labels[0].CategoryLabel != "Tithes" {
		t.Fatalf("Tithes label = %q", preview.Labels[0].CategoryLabel)
	}
	if preview.Labels[1].CategoryID != 0 {
		t.Fatalf("DAY BORN category = %d, want unresolved", preview.Labels[1].CategoryID)
	}
	if !preview.Unresolved["DAY BORN"] {
		t.Fatalf("unresolved = %v, want DAY BORN", preview.Unresolved)
	}
	if len(preview.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one unresolved warning", preview.Warnings)
	}
}

func weeklyWorkbook(t *testing.T, sheetName string, rows [][]string) []byte {
	t.Helper()

	file := excelize.NewFile()
	defer func() {
		_ = file.Close()
	}()
	if err := file.SetSheetName(file.GetSheetName(0), sheetName); err != nil {
		t.Fatalf("name test sheet: %v", err)
	}
	for rowIndex, row := range rows {
		for colIndex, value := range row {
			cell, err := excelize.CoordinatesToCellName(colIndex+1, rowIndex+1)
			if err != nil {
				t.Fatalf("cell name: %v", err)
			}
			if err := file.SetCellValue(sheetName, cell, value); err != nil {
				t.Fatalf("set %s: %v", cell, err)
			}
		}
	}

	buffer, err := file.WriteToBuffer()
	if err != nil {
		t.Fatalf("write test workbook: %v", err)
	}
	return buffer.Bytes()
}
