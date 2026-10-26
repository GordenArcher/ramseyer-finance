package plan

import (
	"testing"
	"time"
)

func TestRowDescriptionAddsServiceOnlyWhenKnown(t *testing.T) {
	t.Parallel()

	withoutService := Row{Label: "Tithe"}
	if withoutService.Description() != "Tithe" {
		t.Fatalf("description = %q, want Tithe", withoutService.Description())
	}

	withService := Row{Label: "Sunday Service Offering", Service: "2nd"}
	if withService.Description() != "Sunday Service Offering (2nd service)" {
		t.Fatalf("description = %q", withService.Description())
	}
}

func TestPlanTotalsAndUndecidedLabels(t *testing.T) {
	t.Parallel()

	date := time.Date(2026, time.January, 4, 0, 0, 0, 0, time.UTC)
	built := &Plan{
		Rows: []Row{
			{Date: date, Key: "TITHES", Label: "Tithe", Category: "Tithes", Source: CategoryStandard, Amount: 100},
			{Date: date, Key: "SCHOLARSHIP", Label: "Scholarship", Source: CategoryUndecided, Amount: 50},
		},
		Labels: []Label{
			{Key: "TITHES", Display: "Tithe", Category: "Tithes", Source: CategoryStandard},
			{Key: "SCHOLARSHIP", Display: "Scholarship", Source: CategoryUndecided},
		},
	}

	if built.Total() != 150 {
		t.Fatalf("total = %.2f, want 150", built.Total())
	}
	undecided := built.UndecidedLabels()
	if len(undecided) != 1 || undecided[0].Key != "SCHOLARSHIP" {
		t.Fatalf("undecided = %v", undecided)
	}
}
