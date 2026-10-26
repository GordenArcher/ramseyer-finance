package importer

import (
	"errors"
	"ramseyer-finance/internal/imports/plan"
	"testing"
)

func testCategories() []CategoryOption {
	return []CategoryOption{
		{ID: 1, Name: "Tithes", Label: "Tithes", NoteRef: "3"},
		{ID: 2, Name: "Special Appeals", Label: "Donations Received / Special Appeals", NoteRef: "6"},
		{ID: 3, Name: "Adult Service Offertory", Label: "Offerings / Adult Service Offertory", NoteRef: "4"},
	}
}

func TestInitialMappingPrefersSavedChoiceThenDefault(t *testing.T) {
	built := &plan.Plan{
		Labels: []plan.Label{
			{Key: "TITHES", Display: "Tithe", Category: "Tithes"},
			{Key: "DAY BORN", Display: "Day Born"},
			{Key: "KUTUNSE PROJECT", Display: "Kutunse Project", Category: "Special Appeals"},
		},
	}
	saved := map[string]int64{
		"DAY BORN":         3,
		"KUTUNSE PROJECT":  999,
		"REMOVED WORKBOOK": 2,
	}

	mapping := InitialMapping(built, testCategories(), saved)
	if mapping["TITHES"] != 1 {
		t.Fatalf("TITHES = %d, want the parser default 1", mapping["TITHES"])
	}
	if mapping["DAY BORN"] != 3 {
		t.Fatalf("DAY BORN = %d, want the saved choice 3", mapping["DAY BORN"])
	}
	if mapping["KUTUNSE PROJECT"] != 2 {
		t.Fatalf("KUTUNSE PROJECT = %d, want fallback to Special Appeals", mapping["KUTUNSE PROJECT"])
	}
	if _, exists := mapping["REMOVED WORKBOOK"]; exists {
		t.Fatal("saved label absent from the workbook should not enter the mapping")
	}
}

func TestResolveMappingRejectsMissingAndUnknownAccounts(t *testing.T) {
	built := &plan.Plan{
		Labels: []plan.Label{
			{Key: "TITHES", Display: "Tithe"},
			{Key: "DAY BORN", Display: "Day Born"},
		},
	}

	mapping, err := resolveMapping(built, testCategories(), map[string]string{
		"TITHES":   "1",
		"DAY BORN": "404",
	})
	if mapping["TITHES"] != 1 {
		t.Fatalf("valid selection was lost: %v", mapping)
	}
	var mappingErr *MappingError
	if !errors.As(err, &mappingErr) {
		t.Fatalf("error = %v, want *MappingError", err)
	}
	if !mappingErr.UnresolvedKeys["DAY BORN"] {
		t.Fatalf("unresolved = %v, want DAY BORN", mappingErr.UnresolvedKeys)
	}
	if mappingErr.Selected["TITHES"] != 1 {
		t.Fatalf("selected = %v, want the valid Tithe choice kept", mappingErr.Selected)
	}
}

func TestResolveMappingRejectsMissingFormValue(t *testing.T) {
	built := &plan.Plan{Labels: []plan.Label{{Key: "TITHES", Display: "Tithe"}}}

	_, err := resolveMapping(built, testCategories(), map[string]string{"TITHES": ""})
	var mappingErr *MappingError
	if !errors.As(err, &mappingErr) {
		t.Fatalf("error = %v, want *MappingError", err)
	}
	if !mappingErr.UnresolvedKeys["TITHES"] {
		t.Fatalf("unresolved = %v, want TITHES", mappingErr.UnresolvedKeys)
	}
}

func TestMergeLabelMapKeepsChoicesForOtherYears(t *testing.T) {
	merged := mergeLabelMap(
		map[string]int64{"OLD LABEL": 4, "TITHES": 1},
		map[string]int64{"TITHES": 3, "DAY BORN": 2},
	)

	if merged["OLD LABEL"] != 4 {
		t.Fatalf("OLD LABEL = %d, want 4", merged["OLD LABEL"])
	}
	if merged["TITHES"] != 3 {
		t.Fatalf("TITHES = %d, want the current choice 3", merged["TITHES"])
	}
	if merged["DAY BORN"] != 2 {
		t.Fatalf("DAY BORN = %d, want 2", merged["DAY BORN"])
	}
}
