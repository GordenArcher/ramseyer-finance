package finance

import (
	"fmt"
	"net/http"
	"ramseyer-finance/internal/db"
	"strconv"
)

// FixedAssetLine is one selected-year PPE category total. The workbook supplies the schedule
// headings, but the app does not import its depreciation formulas or manufacture a charge.
type FixedAssetLine struct {
	CategoryID int64
	Name       string
	Additions  float64
	Disposals  float64
	YearTotal  float64
}

type FixedAssetData struct {
	Active         string
	Year           string
	Years          []int
	Lines          []FixedAssetLine
	TotalAdditions float64
	TotalDisposals float64
	TotalYear      float64
}

type assetYearMovement struct {
	Net       float64
	Additions float64
	Disposals float64
}

func FixedAssetSchedule(w http.ResponseWriter, r *http.Request) {
	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	data, err := buildFixedAssetData(year)
	if err != nil {
		serverError(w, err)
		return
	}
	RenderTemplate(w, "fixed-assets", data)
}

// buildFixedAssetData groups the selected year's dated PPE transactions by asset class. A
// positive entry is an addition and a negative entry is a disposal; no previous year, payment
// flag, depreciation rate, or workbook formula changes the amount the operator entered.
func buildFixedAssetData(year int) (FixedAssetData, error) {
	years, err := reportYears(year)
	if err != nil {
		return FixedAssetData{}, fmt.Errorf("load fixed-asset years: %w", err)
	}
	data := FixedAssetData{Active: "fixed-assets", Year: strconv.Itoa(year), Years: years}

	rows, err := db.DB.Query(`
		SELECT c.id, c.name
		FROM categories c
		JOIN categories parent ON parent.id = c.parent_id
		WHERE c.type = 'asset'
			AND parent.type = 'asset'
			AND parent.name = 'Property, Plant & Equipment'
		ORDER BY c.id
	`)
	if err != nil {
		return FixedAssetData{}, fmt.Errorf("query fixed-asset classes: %w", err)
	}
	type assetClass struct {
		ID   int64
		Name string
	}
	var classes []assetClass
	for rows.Next() {
		var class assetClass
		if err := rows.Scan(&class.ID, &class.Name); err != nil {
			rows.Close()
			return FixedAssetData{}, fmt.Errorf("scan fixed-asset class: %w", err)
		}
		classes = append(classes, class)
	}
	if err := rows.Close(); err != nil {
		return FixedAssetData{}, fmt.Errorf("close fixed-asset classes: %w", err)
	}

	startDate, endDate := yearBounds(year)
	movements := map[int64]assetYearMovement{}
	rows, err = db.DB.Query(`
		SELECT c.id,
			COALESCE(SUM(posting.amount), 0),
			COALESCE(SUM(CASE WHEN posting.amount > 0 THEN posting.amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN posting.amount < 0 THEN -posting.amount ELSE 0 END), 0)
		FROM categories c
		JOIN categories parent ON parent.id = c.parent_id
		JOIN financial_postings posting
			ON posting.category_id = c.id
			AND posting.account_type = 'asset'
			AND posting.date >= ?
			AND posting.date < ?
		WHERE parent.name = 'Property, Plant & Equipment'
		GROUP BY c.id
		ORDER BY c.id
	`, startDate, endDate)
	if err != nil {
		return FixedAssetData{}, fmt.Errorf("query fixed-asset movements: %w", err)
	}
	for rows.Next() {
		var categoryID int64
		var movement assetYearMovement
		if err := rows.Scan(&categoryID, &movement.Net, &movement.Additions, &movement.Disposals); err != nil {
			rows.Close()
			return FixedAssetData{}, fmt.Errorf("scan fixed-asset movement: %w", err)
		}
		movements[categoryID] = movement
	}
	if err := rows.Close(); err != nil {
		return FixedAssetData{}, fmt.Errorf("close fixed-asset movements: %w", err)
	}

	for _, class := range classes {
		movement := movements[class.ID]
		line := FixedAssetLine{
			CategoryID: class.ID,
			Name:       class.Name,
			Additions:  movement.Additions,
			Disposals:  movement.Disposals,
			YearTotal:  movement.Net,
		}
		data.Lines = append(data.Lines, line)
		data.TotalAdditions += line.Additions
		data.TotalDisposals += line.Disposals
		data.TotalYear += line.YearTotal
	}
	return data, nil
}
