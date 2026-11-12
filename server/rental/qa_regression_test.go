package rental

import (
	"fmt"
	"net/http"
	"testing"
)

func TestQANegativeBillChargesRejectedWithoutWrites(t *testing.T) {
	env := setupEnv(t)
	r := env.seedRoom("QA-CHARGES", 1000, 0)
	env.seedTenantViaAPI(r, map[string]interface{}{"name": "合成费用租客"})
	b := issueLandlordBill(t, env, r, "2026-10", nil)
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		for index, key := range []string{"rent", "water_price", "elec_price", "gas_price", "sanitation_fee", "management_fee", "deposit_fee"} {
			// Only the four override fields belong to the create contract.
			if method == http.MethodPost && (key == "sanitation_fee" || key == "management_fee" || key == "deposit_fee") {
				continue
			}
			t.Run(method+"/"+key, func(t *testing.T) {
				path := "/api/rental/bills"
				period := fmt.Sprintf("2027-%02d", index+1)
				if method == http.MethodPut {
					path += "/" + itoa(b.ID)
					period = b.Period
				}
				var before, after int64
				env.db.Model(&Bill{}).Count(&before)
				w := env.do(method, path, map[string]interface{}{"room_id": r, "period": period, key: -0.01})
				if w.Code != http.StatusBadRequest {
					t.Errorf("negative %s: %d %s", key, w.Code, w.Body.String())
				}
				env.db.Model(&Bill{}).Count(&after)
				var stored Bill
				env.db.First(&stored, b.ID)
				if before != after || stored.Rent != b.Rent || stored.TotalAmount != b.TotalAmount {
					t.Error("rejected input changed persisted bill")
				}
			})
		}
	}
	zero := issueLandlordBill(t, env, r, "2026-12", map[string]interface{}{"rent": 0, "water_price": 0, "elec_price": 0, "gas_price": 0})
	if zero.TotalAmount != 0 {
		t.Fatalf("explicit zero charges = %v", zero.TotalAmount)
	}
}

func TestQABalancesDoNotCrossTenancies(t *testing.T) {
	env := setupEnv(t)
	for i, paid := range []float64{2000, 0} {
		r := env.seedRoom(fmt.Sprintf("QA-BAL-%d", i), 1000, 0)
		env.seedTenantViaAPI(r, map[string]interface{}{"name": "同名合成租客"})
		issueLandlordBill(t, env, r, "2026-11", map[string]interface{}{"rent": 1000})
		b := env.fetchBill("2026-11", fmt.Sprintf("QA-BAL-%d", i))
		if paid > 0 {
			w := env.do(http.MethodPost, "/api/rental/bills/"+itoa(uint(num(b["id"])))+"/pay", map[string]interface{}{"amount": paid})
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
		}
	}
	w := env.do(http.MethodGet, "/api/rental/stats?period=2026-11", nil)
	for _, section := range []string{"month", "overall"} {
		if got := num(field(t, decodeBody(t, w), "data", section, "outstanding")); got != 1000 {
			t.Errorf("%s arrears=%v want 1000", section, got)
		}
	}
	w = env.do(http.MethodGet, "/api/rental/analytics", nil)
	if got := num(field(t, decodeBody(t, w), "data", "summary", "arrears")); got != 1000 {
		t.Errorf("analytics=%v want 1000", got)
	}
}

func TestQAUnassignedBalancesStayIndependent(t *testing.T) {
	env := setupEnv(t)
	r := env.seedRoom("QA-LEGACY", 1000, 0)
	for i, paid := range []float64{2000, 0} {
		b := Bill{LedgerID: env.personalLedger, RoomID: r, Period: fmt.Sprintf("2026-%02d", i+1), TenancyKey: fmt.Sprintf("legacy-%d", i), TotalAmount: 1000, Payable: 1000, PaidAmount: paid, TenantName: "同名历史租客"}
		if err := env.db.Create(&b).Error; err != nil {
			t.Fatal(err)
		}
	}
	w := env.do(http.MethodGet, "/api/rental/stats", nil)
	if got := num(field(t, decodeBody(t, w), "data", "overall", "outstanding")); got != 1000 {
		t.Errorf("unassigned balances=%v want 1000", got)
	}
}

func TestQAPortalSummaryUsesAllVisibleFilteredBills(t *testing.T) {
	f := newPortalFixture(t, "QA-PORTAL", "合成门户租客", "2026-01-01", "")
	for i := 1; i <= 12; i++ {
		id := createBillWithTenant(t, f.env, f.roomID, f.env.personalLedger, fmt.Sprintf("2026-%02d", i), "合成门户租客", f.tenantID)
		paid := 0.0
		if i == 12 {
			paid = 600
		}
		if err := f.env.db.Model(&Bill{}).Where("id = ?", id).Updates(map[string]interface{}{"total_amount": 800, "payable": 800, "paid_amount": paid, "status": "unpaid"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A predecessor's overpayment cannot change this renter's totals.
	createBillWithTenant(t, f.env, f.roomID, f.env.personalLedger, "2025-12", "其他租客")
	for _, query := range []string{"?page=1&pageSize=10", "?page=2&pageSize=10", "?period=2026-12&pageSize=10"} {
		w := portalGet(f.env, f.tenant, "/api/rental/portal/bills"+query)
		data := field(t, decodeBody(t, w), "data").(map[string]interface{})
		s, ok := data["summary"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing summary: %s", w.Body.String())
		}
		wantDue, wantCount := 9000.0, 12.0
		if query == "?period=2026-12&pageSize=10" {
			wantDue = 200
			wantCount = 1
		}
		if num(s["outstanding"]) != wantDue || num(s["paid"]) != 600 || num(s["unpaid_count"]) != wantCount {
			t.Fatalf("summary %s: %v", query, s)
		}
	}
}

func TestQATenantIdentityAndDatesRejectBeforeWriting(t *testing.T) {
	env := setupEnv(t)
	r := env.seedRoom("QA-DATES", 1000, 0)
	valid := map[string]interface{}{"room_id": r, "name": "合成日期租客", "move_in_date": "2026-01-01", "lease_end_date": "2026-12-31"}
	w := env.do(http.MethodPost, "/api/rental/tenants", valid)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	id := tenantID(t, decodeBody(t, w))
	cases := []map[string]interface{}{
		{"deposit": -1},
		{"name": " \t "}, {"move_in_date": "not-a-date"}, {"lease_end_date": "2026-02-30"},
		{"move_in_date": "2026-12-01", "lease_end_date": fmt.Sprintf("%d-11-01", 2026)},
		{"move_in_date": "2026-2-01"},
	}
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		for i, bad := range cases {
			t.Run(fmt.Sprintf("%s/%d", method, i), func(t *testing.T) {
				body := map[string]interface{}{}
				for k, v := range valid {
					body[k] = v
				}
				for k, v := range bad {
					body[k] = v
				}
				body["members"] = []map[string]interface{}{{"name": "不能留下的合成同住人"}}
				path := "/api/rental/tenants"
				if method == http.MethodPut {
					path += "/" + itoa(id)
				}
				w := env.do(method, path, body)
				if w.Code != 400 {
					t.Errorf("invalid: %d %s", w.Code, w.Body.String())
				}
				var count, members int64
				env.db.Model(&Tenant{}).Count(&count)
				env.db.Model(&TenantMember{}).Count(&members)
				if count != 1 || members != 0 {
					t.Errorf("partial rows tenants=%d members=%d", count, members)
				}
				stored := env.fetchTenant(id)
				if stored["name"] != valid["name"] || stored["move_in_date"] != valid["move_in_date"] || stored["lease_end_date"] != valid["lease_end_date"] {
					t.Errorf("invalid edit persisted: %v", stored)
				}
			})
		}
	}
	for _, fields := range []map[string]interface{}{{"name": "  合法留空  "}, {"name": "同日起止", "move_in_date": "2026-02-28", "lease_end_date": "2026-02-28"}} {
		fields["room_id"] = r
		w := env.do(http.MethodPost, "/api/rental/tenants", fields)
		if w.Code != 200 {
			t.Fatalf("valid optional dates: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestQAPortalCarryoverSummaryDoesNotDoubleCount(t *testing.T) {
	f := newPortalFixture(t, "QA-CARRY-SUM", "合成结转租客", "2026-01-01", "")
	enableCarryover(t, f.env)
	for _, period := range []string{"2026-01", "2026-02"} {
		issueLandlordBill(t, f.env, f.roomID, period, map[string]interface{}{"rent": 800})
	}
	w := portalGet(f.env, f.tenant, "/api/rental/portal/bills?pageSize=1")
	s := field(t, decodeBody(t, w), "data", "summary").(map[string]interface{})
	if num(s["outstanding"]) != 1600 || num(s["paid"]) != 0 || num(s["unpaid_count"]) != 2 {
		t.Fatalf("carry counted twice: %v", s)
	}
}

func TestQAPortalSummaryAtPageBoundaries(t *testing.T) {
	f := newPortalFixture(t, "QA-BOUNDARIES", "合成边界租客", "2026-01-01", "")
	for i := 0; i <= 12; i++ {
		if i > 0 {
			id := createBillWithTenant(t, f.env, f.roomID, f.env.personalLedger, fmt.Sprintf("2026-%02d", i), "合成边界租客", f.tenantID)
			if err := f.env.db.Model(&Bill{}).Where("id = ?", id).Updates(map[string]interface{}{"total_amount": 800, "payable": 800, "paid_amount": 0}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if i != 0 && i != 1 && i != 10 && i != 11 && i != 12 {
			continue
		}
		w := portalGet(f.env, f.tenant, "/api/rental/portal/bills?pageSize=10")
		data := field(t, decodeBody(t, w), "data").(map[string]interface{})
		s := data["summary"].(map[string]interface{})
		if num(data["total"]) != float64(i) || num(s["outstanding"]) != float64(800*i) || num(s["unpaid_count"]) != float64(i) {
			t.Fatalf("count %d: %v", i, data)
		}
	}
}

func TestQABalanceGroupingPreservesLeaseAndLedgerBoundaries(t *testing.T) {
	// Amounts represent actual current charges and payments, not accumulated payable.
	cases := []struct {
		name  string
		bills []Bill
		want  float64
	}{
		{"same lease across months", []Bill{{ID: 1, LedgerID: 1, RoomID: 1, RecipientTenantIDs: []uint{1}, TotalAmount: 800, Payable: 800}, {ID: 2, LedgerID: 1, RoomID: 1, RecipientTenantIDs: []uint{1}, TotalAmount: 800, Payable: 1600, PaidAmount: 1700}}, 0},
		{"turnover same room", []Bill{{ID: 1, LedgerID: 1, RoomID: 1, RecipientTenantIDs: []uint{1}, TotalAmount: 1000, PaidAmount: 2000}, {ID: 2, LedgerID: 1, RoomID: 1, RecipientTenantIDs: []uint{2}, TotalAmount: 1000}}, 1000},
		{"different ledger", []Bill{{ID: 1, LedgerID: 1, RoomID: 1, RecipientTenantIDs: []uint{1}, TotalAmount: 1000, PaidAmount: 2000}, {ID: 2, LedgerID: 2, RoomID: 1, RecipientTenantIDs: []uint{1}, TotalAmount: 1000}}, 1000},
		{"joint recipients order", []Bill{{ID: 1, LedgerID: 1, RoomID: 1, RecipientTenantIDs: []uint{1, 2}, TotalAmount: 1000, PaidAmount: 2000}, {ID: 2, LedgerID: 1, RoomID: 1, RecipientTenantIDs: []uint{2, 1}, TotalAmount: 1000}}, 0},
		{"changed joint recipients", []Bill{{ID: 1, LedgerID: 1, RoomID: 1, RecipientTenantIDs: []uint{1, 2}, TotalAmount: 1000, PaidAmount: 2000}, {ID: 2, LedgerID: 1, RoomID: 1, RecipientTenantIDs: []uint{2, 3}, TotalAmount: 1000}}, 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := outstandingByTenancy(tc.bills, false); got != tc.want {
				t.Fatalf("outstanding=%v want %v", got, tc.want)
			}
		})
	}
}
