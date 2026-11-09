package bulk

import "testing"

func TestBuildEntryTemplateStartsWithTheHeaderRow(t *testing.T) {
	workbook, err := BuildEntryTemplate("income", []Category{{ID: 1, Name: "Offerings"}})
	if err != nil {
		t.Fatalf("BuildEntryTemplate failed: %v", err)
	}
	defer workbook.Close()

	if names := workbook.GetSheetList(); len(names) == 0 || names[0] != "income" {
		t.Fatalf("template sheets = %v, want income first", names)
	}
	if value, _ := workbook.GetCellValue("income", "B1"); value != "Description" {
		t.Fatalf("template description header = %q", value)
	}
}
