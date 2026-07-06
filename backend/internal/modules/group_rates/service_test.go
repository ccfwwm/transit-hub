package group_rates

import (
	"math"
	"testing"
	"time"
)

func TestRateRowsIncludeConvertedBalanceAndConnectedOwnGroups(t *testing.T) {
	now := time.Now()
	balance := 25.0
	rows := rateRowsFromRecords([]snapshotRecord{{
		SiteID:             "site-1",
		SiteName:           "pool.example.com",
		GroupID:            "gpt-4o",
		GroupName:          "GPT-4o",
		Platform:           "sub2api",
		Type:               "openai",
		Mapped:             true,
		MappedOwnGroups:    []string{"PLUS", "PRO"},
		Multiplier:         0.8,
		RechargeRate:       2,
		Balance:            &balance,
		CreatedAt:          now,
		PreviousMultiplier: floatPtr(1.0),
	}})

	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}
	row := rows[0]
	if row.CurrentMultiplier != 1.6 {
		t.Fatalf("expected converted multiplier 1.6, got %v", row.CurrentMultiplier)
	}
	if row.Balance == nil || *row.Balance != balance {
		t.Fatalf("expected converted balance 25, got %v", row.Balance)
	}
	if len(row.MappedOwnGroups) != 2 || row.MappedOwnGroups[0] != "PLUS" || row.MappedOwnGroups[1] != "PRO" {
		t.Fatalf("expected mapped own groups, got %+v", row.MappedOwnGroups)
	}
	if row.Delta == nil || math.Abs(*row.Delta-(-0.2)) > 1e-9 {
		t.Fatalf("expected raw multiplier delta -0.2, got %v", row.Delta)
	}
}

func floatPtr(value float64) *float64 { return &value }
