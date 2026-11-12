package rental

import (
	"net/http"
	"strings"
	"testing"
)

func TestRentExplanationQuarterlyNewTenantAndFrozenDates(t *testing.T) {
	env := setupEnv(t)
	room := env.seedRoom("TEST-DATES", 500, 0)
	w := env.do(http.MethodPost, "/api/rental/tenants", map[string]interface{}{"room_id": room, "name": "合成租客", "move_in_date": "2026-11-12", "lease_end_date": "2027-05-11", "pay_cycle": "quarterly"})
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	bill := issueLandlordBill(t, env, room, "2026-11", nil)
	if bill.Rent != 1316.67 || bill.RentStartDate != "2026-11-12" || bill.RentEndDate != "2027-01-31" {
		t.Fatalf("wrong rent dates: %#v", bill)
	}
	if !strings.Contains(bill.RentCalculation, "500 × 19 ÷ 30 = 316.67") || !strings.Contains(bill.RentCalculation, "2027-01-01 ~ 2027-01-31") {
		t.Fatal(bill.RentCalculation)
	}
	if err := env.db.Model(&Tenant{}).Where("room_id = ?", room).Update("move_in_date", "2026-11-01").Error; err != nil {
		t.Fatal(err)
	}
	var saved Bill
	if err := env.db.First(&saved, bill.ID).Error; err != nil {
		t.Fatal(err)
	}
	fillLegacyRentBasis(env.db, &saved)
	if saved.RentStartDate != "2026-11-12" || saved.Rent != 1316.67 {
		t.Fatal("snapshot changed with tenancy")
	}
	legacy := saved
	legacy.RentStartDate, legacy.RentEndDate, legacy.RentCalculation = "", "", ""
	fillLegacyRentBasis(env.db, &legacy)
	if legacy.RentBasisSource != "current_lease_reference" || legacy.Rent != saved.Rent {
		t.Fatal("legacy reference changed amount or was not marked")
	}
}
