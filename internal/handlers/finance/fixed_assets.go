package finance

import (
	"fmt"
	"math"
	"net/http"
	"ramseyer-finance/internal/db"
	"sort"
	"strconv"
	"strings"
)

// FixedAssetLine is one workbook asset-class roll-forward. Additions and disposals come
// from signed asset movements, while depreciation is calculated at the standard PCG rate
// and capped at the remaining depreciable amount so an old asset can never acquire a
// negative carrying value.
type FixedAssetLine struct {
	CategoryID            int64
	Name                  string
	Rate                  float64
	OpeningCost           float64
	Additions             float64
	Disposals             float64
	ClosingCost           float64
	OpeningAccumulatedDep float64
	Charge                float64
	ClosingAccumulatedDep float64
	CarryingAmount        float64
}

type FixedAssetData struct {
	Active                     string
	Year                       string
	Years                      []int
	Lines                      []FixedAssetLine
	TotalOpeningCost           float64
	TotalAdditions             float64
	TotalDisposals             float64
	TotalClosingCost           float64
	TotalOpeningAccumulatedDep float64
	TotalCharge                float64
	TotalClosingAccumulatedDep float64
	TotalCarryingAmount        float64
	PPECarryingAmount          float64
	IntangibleCarryingAmount   float64
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

// buildFixedAssetData reproduces Note 21's cost, accumulated-depreciation, annual-charge,
// and carrying-amount columns from transaction history. The workbook applies a full-year
// class rate to closing cost, so the implementation follows that convention consistently
// for comparability rather than inventing monthly proration that the source document does
// not contain.
func buildFixedAssetData(year int) (FixedAssetData, error) {
	years, err := reportYears(year)
	if err != nil {
		return FixedAssetData{}, fmt.Errorf("load fixed-asset years: %w", err)
	}
	data := FixedAssetData{Active: "fixed-assets", Year: strconv.Itoa(year), Years: years}

	rows, err := db.DB.Query(`
		SELECT c.id, c.name, parent.name
		FROM categories c
		JOIN categories parent ON parent.id = c.parent_id
		WHERE c.type = 'asset'
			AND parent.type = 'asset'
			AND parent.name IN ('Property, Plant & Equipment', 'Intangible Assets')
		ORDER BY CASE parent.name WHEN 'Property, Plant & Equipment' THEN 0 ELSE 1 END, c.id
	`)
	if err != nil {
		return FixedAssetData{}, fmt.Errorf("query fixed-asset classes: %w", err)
	}
	type assetClass struct {
		ID     int64
		Name   string
		Parent string
	}
	var classes []assetClass
	for rows.Next() {
		var class assetClass
		if err := rows.Scan(&class.ID, &class.Name, &class.Parent); err != nil {
			rows.Close()
			return FixedAssetData{}, fmt.Errorf("scan fixed-asset class: %w", err)
		}
		classes = append(classes, class)
	}
	if err := rows.Close(); err != nil {
		return FixedAssetData{}, fmt.Errorf("close fixed-asset classes: %w", err)
	}

	movements := map[int64]map[int]assetYearMovement{}
	rows, err = db.DB.Query(`
		SELECT c.id, CAST(strftime('%Y', posting.date) AS INTEGER),
			COALESCE(SUM(posting.amount), 0),
			COALESCE(SUM(CASE WHEN posting.amount > 0 THEN posting.amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN posting.amount < 0 THEN -posting.amount ELSE 0 END), 0)
		FROM categories c
		JOIN categories parent ON parent.id = c.parent_id
		JOIN financial_postings posting ON posting.category_id = c.id AND posting.account_type = 'asset'
		WHERE parent.name IN ('Property, Plant & Equipment', 'Intangible Assets')
		GROUP BY c.id, CAST(strftime('%Y', posting.date) AS INTEGER)
		ORDER BY c.id
	`)
	if err != nil {
		return FixedAssetData{}, fmt.Errorf("query fixed-asset movements: %w", err)
	}
	for rows.Next() {
		var categoryID int64
		var movementYear int
		var movement assetYearMovement
		if err := rows.Scan(&categoryID, &movementYear, &movement.Net, &movement.Additions, &movement.Disposals); err != nil {
			rows.Close()
			return FixedAssetData{}, fmt.Errorf("scan fixed-asset movement: %w", err)
		}
		if movements[categoryID] == nil {
			movements[categoryID] = map[int]assetYearMovement{}
		}
		movements[categoryID][movementYear] = movement
	}
	if err := rows.Close(); err != nil {
		return FixedAssetData{}, fmt.Errorf("close fixed-asset movements: %w", err)
	}

	for _, class := range classes {
		line := FixedAssetLine{CategoryID: class.ID, Name: class.Name, Rate: depreciationRate(class.Name, class.Parent)}
		yearMovements := movements[class.ID]
		movementYears := make([]int, 0, len(yearMovements))
		for movementYear := range yearMovements {
			movementYears = append(movementYears, movementYear)
		}
		sort.Ints(movementYears)
		firstYear := year
		if len(movementYears) > 0 && movementYears[0] < firstYear {
			firstYear = movementYears[0]
		}

		var cost, accumulatedDep float64
		for calculationYear := firstYear; calculationYear <= year; calculationYear++ {
			movement := yearMovements[calculationYear]
			if calculationYear == year {
				line.OpeningCost = cost
				line.OpeningAccumulatedDep = accumulatedDep
				line.Additions = movement.Additions
				line.Disposals = movement.Disposals
			}
			cost += movement.Net
			if cost < 0 {
				cost = 0
			}
			remaining := math.Max(cost-accumulatedDep, 0)
			charge := math.Min(cost*line.Rate, remaining)
			if calculationYear == year {
				line.Charge = charge
			}
			accumulatedDep = math.Min(accumulatedDep+charge, cost)
		}
		line.ClosingCost = cost
		line.ClosingAccumulatedDep = accumulatedDep
		line.CarryingAmount = math.Max(cost-accumulatedDep, 0)
		data.Lines = append(data.Lines, line)
		data.TotalOpeningCost += line.OpeningCost
		data.TotalAdditions += line.Additions
		data.TotalDisposals += line.Disposals
		data.TotalClosingCost += line.ClosingCost
		data.TotalOpeningAccumulatedDep += line.OpeningAccumulatedDep
		data.TotalCharge += line.Charge
		data.TotalClosingAccumulatedDep += line.ClosingAccumulatedDep
		data.TotalCarryingAmount += line.CarryingAmount
		if class.Parent == "Intangible Assets" {
			data.IntangibleCarryingAmount += line.CarryingAmount
		} else {
			data.PPECarryingAmount += line.CarryingAmount
		}
	}
	return data, nil
}

func depreciationRate(name, parent string) float64 {
	if parent == "Intangible Assets" || strings.EqualFold(name, "Software") {
		return 0.20
	}
	lowerName := strings.ToLower(name)
	switch {
	case lowerName == "land":
		return 0
	case strings.Contains(lowerName, "building"):
		return 0.02
	case strings.Contains(lowerName, "motor vehicle"):
		return 0.20
	case strings.Contains(lowerName, "computer"), strings.Contains(lowerName, "church instrument"), strings.Contains(lowerName, "musical instrument"):
		return 0.20
	case strings.Contains(lowerName, "plant"), strings.Contains(lowerName, "furniture"), strings.Contains(lowerName, "fixture"), strings.Contains(lowerName, "office equipment"):
		return 0.10
	default:
		return 0
	}
}
