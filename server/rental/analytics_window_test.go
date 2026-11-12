package rental

import (
	"fmt"
	"net/http"
	"testing"
)

func TestAnalyticsCompositionMatchesDisplayedMonthWindow(t *testing.T) {
	for _, months := range []int{3, 6, 12, 24} {
		t.Run(fmt.Sprint(months), func(t *testing.T) {
			env := setupEnv(t)
			current := currentPeriodStr()
			start := addMonths(current, 1-months)
			for i, row := range []struct {
				period string
				amount float64
				ledger uint
			}{
				{start, 300, env.personalLedger},
				{current, 1000, env.personalLedger},
				{addMonths(start, -1), 70, env.personalLedger},
				{addMonths(current, 1), 9000, env.personalLedger},
				{current, 4000, env.personalLedger + 1000},
			} {
				bill := Bill{LedgerID: row.ledger, RoomID: uint(i + 1), Period: row.period, TotalAmount: row.amount, Payable: row.amount, TenancyKey: fmt.Sprint(i)}
				if err := env.db.Create(&bill).Error; err != nil {
					t.Fatal(err)
				}
				item := BillItem{LedgerID: row.ledger, BillID: bill.ID, FeeKey: "rent", Name: "租金", Amount: row.amount}
				if err := env.db.Create(&item).Error; err != nil {
					t.Fatal(err)
				}
			}
			w := env.do(http.MethodGet, fmt.Sprintf("/api/rental/analytics?months=%d", months), nil)
			if w.Code != http.StatusOK {
				t.Fatal(w.Body.String())
			}
			data := field(t, decodeBody(t, w), "data").(map[string]interface{})
			if billed := num(data["summary"].(map[string]interface{})["billed"]); billed != 1300 {
				t.Fatalf("displayed window billed=%v want 1300", billed)
			}
			items := data["items"].([]interface{})
			if len(items) != 1 || num(items[0].(map[string]interface{})["amount"]) != 1300 {
				t.Errorf("composition must exclude future, expired and other-ledger bills: %v", items)
			}
			series := data["series"].([]interface{})
			if len(series) != months || series[0].(map[string]interface{})["period"] != start || series[months-1].(map[string]interface{})["period"] != current {
				t.Errorf("unexpected displayed month window: %v", series)
			}
		})
	}
}
