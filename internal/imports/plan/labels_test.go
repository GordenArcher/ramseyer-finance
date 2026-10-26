package plan

import "testing"

func TestKeyFoldsLabelSpellings(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"V.T.O.":                   "VTO",
		"Second  Offering":         "ADULT SERVICE OFFERTORY",
		"SUNDAY OFFERING":          "ADULT SERVICE OFFERTORY",
		"Children's Service offer": "CHILDREN'S SERVICE OFFERINGS",
		"4TH MINI Harvest":         "MINI HARVESTS",
		"J.Y. OFFERING":            "JY OFFERINGS",
		"KUTUNSE PROJECT OFFERING": "KUTUNSE PROJECT",
		"Day Born Pledges":         "DAY BORN",
		"FLOOD VICTIMS-Offering":   "FLOOD VICTIMS",
		"GMF  OFFERING":            "GMF OFFERING",
	}

	for label, want := range cases {
		if got := Key(label); got != want {
			t.Errorf("Key(%q) = %q, want %q", label, got, want)
		}
	}
}

func TestDefaultCategoryMarksAmbiguity(t *testing.T) {
	t.Parallel()

	category, source := DefaultCategory("TITHES")
	if category != "Tithes" || source != CategoryStandard {
		t.Fatalf("TITHES = %q/%q, want Tithes/standard", category, source)
	}

	category, source = DefaultCategory("KUTUNSE PROJECT")
	if category != "Special Appeals" || source != CategoryHeuristic {
		t.Fatalf("KUTUNSE PROJECT = %q/%q, want Special Appeals/heuristic", category, source)
	}

	for _, key := range []string{"BIRTHDAY OFFERING", "SCHOLARSHIP", "DAY BORN", "SPEAKING"} {
		category, source = DefaultCategory(key)
		if category != "Special Appeals" || source != CategoryHeuristic {
			t.Fatalf("%s = %q/%q, want Special Appeals/heuristic", key, category, source)
		}
	}

	category, source = DefaultCategory("UNRECOGNISED COLLECTION")
	if category != "" || source != CategoryUndecided {
		t.Fatalf("UNRECOGNISED COLLECTION = %q/%q, want empty/undecided", category, source)
	}
}
