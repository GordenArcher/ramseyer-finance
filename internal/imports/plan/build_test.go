package plan

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

type testSheet struct {
	name string
	rows [][]string
}

func TestBuildReadsWeeklyGapsAndSkipsDerivedSheets(t *testing.T) {
	t.Parallel()

	data := buildTestWorkbook(t, []testSheet{
		{
			name: "JAN TO JULY",
			rows: [][]string{
				{"", "2026-01-04", "2026-01-11", "2026-01-18", "2026-01-25", "2026-03-08"},
				{"Sunday Service Offering", "100", "", "", "", "300"},
				{"Tithe", "400", "", "", "", "600"},
				{"TOTAL", "500", "", "", "", "900"},
			},
		},
		{
			name: "08MAR",
			rows: [][]string{
				{"", "1ST", "2ND", "TOTAL"},
				{"Sunday Service Offering", "300", "0", "300"},
				{"Tithe", "600", "", "600"},
				{"TOTAL", "900", "0", "900"},
			},
		},
		{
			name: "CEMENT ACCOUNT",
			rows: [][]string{
				{"", "800"},
				{"NAME", "AMOUNT"},
				{"A MEMBER", "400"},
				{"ANOTHER MEMBER", "400"},
			},
		},
		{
			name: "MAY",
			rows: [][]string{
				{"", "Sunday Service Offering", "300"},
				{"", "Tithe", "600"},
				{"", "TOTAL", "900"},
			},
		},
	})

	result, err := Build(data, Options{FileName: "synthetic.xlsx"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if result.Year != 2026 {
		t.Fatalf("year = %d, want 2026", result.Year)
	}
	if len(result.Rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(result.Rows))
	}
	if result.Total() != 1400 {
		t.Fatalf("total = %.2f, want 1400", result.Total())
	}

	kinds := make(map[string]SheetKind, len(result.Sheets))
	for _, report := range result.Sheets {
		kinds[report.Name] = report.Kind
	}
	if kinds["JAN TO JULY"] != KindSummary {
		t.Fatalf("summary kind = %q", kinds["JAN TO JULY"])
	}
	if kinds["08MAR"] != KindWeekly {
		t.Fatalf("weekly kind = %q", kinds["08MAR"])
	}
	if kinds["CEMENT ACCOUNT"] != KindPledgeList {
		t.Fatalf("pledge kind = %q", kinds["CEMENT ACCOUNT"])
	}
	if kinds["MAY"] != KindUndatedSummary {
		t.Fatalf("undated summary kind = %q", kinds["MAY"])
	}

	if len(result.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", result.Warnings)
	}
	if len(result.UndecidedLabels()) != 0 {
		t.Fatalf("unexpected undecided labels: %v", result.UndecidedLabels())
	}
}

func TestBuildKeepsSummaryYearOverOverride(t *testing.T) {
	t.Parallel()

	data := buildTestWorkbook(t, []testSheet{
		{
			name: "JAN TO JULY",
			rows: [][]string{
				{"", "2026-01-04", "2026-01-11", "2026-01-18", "2026-01-25", "2026-02-01"},
				{"Tithe", "400", "", "", "", ""},
				{"TOTAL", "400", "", "", "", ""},
			},
		},
	})

	result, err := Build(data, Options{FileName: "synthetic.xlsx", Year: 2025})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.Year != 2026 {
		t.Fatalf("year = %d, want the summary year 2026", result.Year)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one override warning", result.Warnings)
	}
}

func TestBuildMarksUnknownLabelsUndecided(t *testing.T) {
	t.Parallel()

	data := buildTestWorkbook(t, []testSheet{
		{
			name: "08MAR",
			rows: [][]string{
				{"", "1ST", "2ND", "TOTAL"},
				{"UNRECOGNISED COLLECTION", "500", "", "500"},
				{"TOTAL", "500", "", "500"},
			},
		},
	})

	result, err := Build(data, Options{FileName: "synthetic.xlsx", Year: 2026})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	undecided := result.UndecidedLabels()
	if len(undecided) != 1 || undecided[0].Key != "UNRECOGNISED COLLECTION" {
		t.Fatalf("undecided = %v, want UNRECOGNISED COLLECTION", undecided)
	}
}

// buildTestWorkbook creates a real xlsx in memory. Going through excelize keeps the test
// on the same reader path as an upload instead of hand-building row slices the production
// code would never see.
func buildTestWorkbook(t *testing.T, sheets []testSheet) []byte {
	t.Helper()

	file := excelize.NewFile()
	defer func() {
		_ = file.Close()
	}()

	for index, sheet := range sheets {
		if index == 0 {
			file.SetSheetName(file.GetSheetName(0), sheet.name)
		} else if _, err := file.NewSheet(sheet.name); err != nil {
			t.Fatalf("create sheet %q: %v", sheet.name, err)
		}

		for rowIndex, row := range sheet.rows {
			for colIndex, value := range row {
				if value == "" {
					continue
				}
				cell, err := excelize.CoordinatesToCellName(colIndex+1, rowIndex+1)
				if err != nil {
					t.Fatalf("cell name: %v", err)
				}
				if err := file.SetCellValue(sheet.name, cell, value); err != nil {
					t.Fatalf("set %s!%s: %v", sheet.name, cell, err)
				}
			}
		}
	}

	var buffer bytes.Buffer
	if err := file.Write(&buffer); err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	return buffer.Bytes()
}
