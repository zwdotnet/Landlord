package rental

import (
	"fmt"
	"net/http"
	"testing"

	"smallgo/server/database"
)

func issueLandlordBill(t *testing.T, env *testEnv, roomID uint, period string, extra map[string]interface{}) Bill {
	t.Helper()
	body := map[string]interface{}{"room_id": roomID, "period": period}
	for k, v := range extra {
		body[k] = v
	}
	w := env.do(http.MethodPost, "/api/rental/bills", body)
	if w.Code != http.StatusOK {
		t.Fatalf("issue: %d %s", w.Code, w.Body.String())
	}
	id := uint(num(field(t, decodeBody(t, w), "data", "id")))
	var bill Bill
	if err := env.db.First(&bill, id).Error; err != nil {
		t.Fatal(err)
	}
	return bill
}

func TestLandlordSameNameTurnoverKeepsBillsPaymentsAndDepositsSeparate(t *testing.T) {
	f := newPortalFixture(t, "L101", "同名租客", "2026-09-01", "")
	env := f.env
	if err := migrateBillOwnership(env.db); err != nil {
		t.Fatal(err)
	}
	enableCarryover(t, env)
	old := issueLandlordBill(t, env, f.roomID, "2026-09", map[string]interface{}{"water_now": 100, "elec_now": 1000})
	if w := env.do(http.MethodPost, fmt.Sprintf("/api/rental/bills/%d/pay", old.ID), map[string]interface{}{"amount": 100}); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if w := env.do(http.MethodPost, fmt.Sprintf("/api/rental/tenants/%d/checkout", f.tenantID), map[string]interface{}{"move_out_date": "2026-09-15"}); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	w := env.do(http.MethodPost, "/api/rental/tenants", map[string]interface{}{
		"room_id": f.roomID, "name": "同名租客", "move_in_date": "2026-09-16", "deposit": 2000,
	})
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	newID := tenantID(t, decodeBody(t, w))
	uid, token := registerTenant(t, env, "successor")
	bindTenantAccount(t, env, env.token, newID, uid)
	current := issueLandlordBill(t, env, f.roomID, "2026-09", map[string]interface{}{"rent": 500})
	if current.TenancyKey == old.TenancyKey || len(current.RecipientTenantIDs) != 1 || current.RecipientTenantIDs[0] != newID {
		t.Fatal("tenancy identity was not persisted")
	}
	if current.WaterLast != 100 || current.WaterFee != 0 || current.ElecLast != 1000 || current.ElecFee != 0 {
		t.Fatal("successor was charged predecessor meter usage")
	}
	if current.DepositFee != 2000 || current.CarryIn != 0 {
		t.Fatalf("successor deposit/carry = %v/%v", current.DepositFee, current.CarryIn)
	}
	for _, path := range []string{fmt.Sprintf("/api/rental/portal/bills/%d", old.ID), fmt.Sprintf("/api/rental/portal/bills/%d/receipt", old.ID)} {
		if w := portalGet(env, token, path); w.Code != http.StatusNotFound {
			t.Fatalf("predecessor leaked: %d %s", w.Code, w.Body.String())
		}
	}
	for _, pair := range []struct {
		token string
		id    uint
	}{{f.tenant, old.ID}, {token, current.ID}} {
		w := portalGet(env, pair.token, "/api/rental/portal/bills")
		if w.Code != http.StatusOK {
			t.Fatal(w.Body.String())
		}
		items := env.items(decodeBody(t, w))
		if len(items) != 1 || uint(num(items[0].(map[string]interface{})["id"])) != pair.id {
			t.Fatalf("wrong bill set: %s", w.Body.String())
		}
	}
	w = portalGet(env, token, "/api/rental/portal/payments")
	if len(env.items(decodeBody(t, w))) != 0 {
		t.Fatal("predecessor payment leaked")
	}
	next := issueLandlordBill(t, env, f.roomID, "2026-10", nil)
	if next.DepositFee != 0 || next.CarryIn != current.Payable {
		t.Fatalf("successor next bill deposit/carry = %v/%v", next.DepositFee, next.CarryIn)
	}
	// Recalculating the room and collecting the predecessor's debt must not
	// change the successor's carry chain.
	if _, err := recalcCarryoverFrom(env.db, &Room{ID: f.roomID, LedgerID: env.personalLedger}, "2026-09"); err != nil {
		t.Fatal(err)
	}
	var after Bill
	env.db.First(&after, next.ID)
	if after.CarryIn != current.Payable {
		t.Fatal("carry crossed tenancy boundary")
	}
	duplicate := env.do(http.MethodPost, "/api/rental/bills", map[string]interface{}{"room_id": f.roomID, "period": "2026-09"})
	if duplicate.Code != http.StatusBadRequest {
		t.Fatal("duplicate tenancy bill was allowed")
	}
}

func TestLandlordFutureRenewalsUseDatedRent(t *testing.T) {
	env := setupEnv(t)
	roomID := env.seedRoom("L201", 2000, 0)
	tid := env.seedTenantWithLease(roomID, "租客", "2030-12-31")
	old := issueLandlordBill(t, env, roomID, "2030-10", nil)
	for _, rent := range []float64{2500, 3000} {
		w := env.do(http.MethodPost, fmt.Sprintf("/api/rental/tenants/%d/renew", tid), map[string]interface{}{"months": 12, "rent": rent})
		if w.Code != http.StatusOK {
			t.Fatal(w.Body.String())
		}
		data := field(t, decodeBody(t, w), "data", "renewal").(map[string]interface{})
		wantStart := "2031-01-01"
		wantBefore := 2000.0
		if rent == 3000 {
			wantStart = "2032-01-01"
			wantBefore = 2500
		}
		if data["effective_from"] != wantStart || num(data["rent_before"]) != wantBefore {
			t.Fatalf("wrong dated renewal: %v", data)
		}
	}
	for _, tc := range []struct {
		period string
		rent   float64
	}{{"2030-11", 2000}, {"2031-01", 2500}, {"2032-01", 3000}} {
		bill := issueLandlordBill(t, env, roomID, tc.period, nil)
		if bill.Rent != tc.rent {
			t.Fatalf("%s rent=%v want=%v", tc.period, bill.Rent, tc.rent)
		}
	}
	var unchanged Bill
	env.db.First(&unchanged, old.ID)
	if unchanged.Rent != 2000 {
		t.Fatal("issued bill was repriced")
	}
	var room Room
	env.db.First(&room, roomID)
	if room.DefaultRent != 2000 {
		t.Fatal("future renewal changed room defaults")
	}
}

func TestLandlordMidPeriodRentChangeUsesActualDays(t *testing.T) {
	env := setupEnv(t)
	room := env.seedRoom("L301", 2000, 0)
	tid := env.seedTenantWithLease(room, "租客", "2030-09-15")
	w := env.do(http.MethodPost, fmt.Sprintf("/api/rental/tenants/%d/renew", tid), map[string]interface{}{"months": 12, "rent": 2500})
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	bill := issueLandlordBill(t, env, room, "2030-09", nil)
	if bill.Rent != 2250 {
		t.Fatalf("mixed rate rent=%v want=2250", bill.Rent)
	}
	w = env.do(http.MethodPut, fmt.Sprintf("/api/rental/bills/%d", bill.ID), map[string]interface{}{"room_id": room, "period": "2030-09", "rent": 2260, "remark": "房东确认调整"})
	if w.Code != http.StatusOK || num(field(t, decodeBody(t, w), "data", "rent")) != 2260 {
		t.Fatal("landlord adjustment not retained")
	}
}

func TestLandlordPartialMonthsUseCalendarDays(t *testing.T) {
	env := setupEnv(t)
	for i, tc := range []struct {
		period, start, end string
		rent, want         float64
	}{
		{"2024-02", "2024-02-15", "2024-02-29", 2900, 1500},
		{"2026-02", "2026-02-15", "2026-02-28", 2800, 1400},
		{"2026-01", "2026-01-31", "2026-02-28", 3100, 100},
		{"2026-09", "2026-08-01", "2026-09-15", 3000, 1500},
	} {
		room := env.seedRoom(fmt.Sprintf("P%d", i), tc.rent, 0)
		w := env.do(http.MethodPost, "/api/rental/tenants", map[string]interface{}{"room_id": room, "name": "日租折算", "move_in_date": tc.start, "lease_end_date": tc.end})
		if w.Code != http.StatusOK {
			t.Fatal(w.Body.String())
		}
		bill := issueLandlordBill(t, env, room, tc.period, nil)
		if bill.Rent != tc.want {
			t.Fatalf("%s rent=%v want=%v", tc.period, bill.Rent, tc.want)
		}
	}
}

func TestLandlordBackfillCannotAssignBillBeforeCurrentTenancy(t *testing.T) {
	env := setupEnv(t)
	room := env.seedRoom("BACKFILL", 2000, 0)
	w := env.do(http.MethodPost, "/api/rental/tenants", map[string]interface{}{"room_id": room, "name": "新租客", "move_in_date": "2026-10-01"})
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	bill := issueLandlordBill(t, env, room, "2026-09", nil)
	if len(bill.RecipientTenantIDs) != 0 {
		t.Fatal("historical bill assigned to successor")
	}
	w = env.do(http.MethodPost, "/api/rental/bills/generate", map[string]interface{}{"period": "2026-08"})
	if num(field(t, decodeBody(t, w), "data", "created")) != 0 {
		t.Fatal("automatic bill generated before move-in")
	}
}

func TestLandlordMeterRecordsFeedBillsWithoutFutureReadings(t *testing.T) {
	env := setupEnv(t)
	room := env.seedRoom("L401", 2000, 0)
	env.seedTenantWithLease(room, "租客", "")
	for _, r := range []struct {
		period      string
		water, elec float64
	}{{"2026-08", 100, 1000}, {"2026-09", 110, 1100}, {"2035-01", 9999, 9999}} {
		w := env.do(http.MethodPost, "/api/rental/meter-records", map[string]interface{}{"room_id": room, "period": r.period, "water": r.water, "elec": r.elec})
		if w.Code != http.StatusOK {
			t.Fatal(w.Body.String())
		}
	}
	// A pre-created future bill must not become the previous reading either.
	if err := env.db.Create(&Bill{LedgerID: env.personalLedger, RoomID: room, Period: "2036-01", RoomNo: "L401", WaterNow: 10000, ElecNow: 10000}).Error; err != nil {
		t.Fatal(err)
	}
	bill := issueLandlordBill(t, env, room, "2026-09", nil)
	if bill.WaterLast != 100 || bill.WaterNow != 110 || bill.ElecLast != 1000 || bill.ElecNow != 1100 || bill.WaterFee != 50 || bill.ElecFee != 120 {
		t.Fatalf("meter snapshot wrong: %+v", bill)
	}
	// Correcting a source reading doesn't silently rewrite an issued bill.
	env.db.Model(&MeterRecord{}).Where("room_id = ? AND period = ?", room, "2026-09").Update("water", 120)
	var after Bill
	env.db.First(&after, bill.ID)
	if after.WaterFee != 50 {
		t.Fatal("source correction rewrote issued bill")
	}
}

func TestLandlordLegacyBillAssignmentIsExplicitAndOneTime(t *testing.T) {
	f := newPortalFixture(t, "L501", "张三", "2026-01-01", "")
	legacy := createBillWithTenant(t, f.env, f.roomID, f.env.personalLedger, "2026-09", "张三")
	if w := portalGet(f.env, f.tenant, fmt.Sprintf("/api/rental/portal/bills/%d", legacy)); w.Code != http.StatusNotFound {
		t.Fatal("legacy name granted access")
	}
	path := fmt.Sprintf("/api/rental/bills/%d/recipients", legacy)
	w := f.env.do(http.MethodPut, path, map[string]interface{}{"tenant_ids": []uint{f.tenantID}})
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if w := portalGet(f.env, f.tenant, fmt.Sprintf("/api/rental/portal/bills/%d", legacy)); w.Code != http.StatusOK {
		t.Fatalf("explicit assignment not visible: %s", w.Body.String())
	}
	if w := f.env.do(http.MethodPut, path, map[string]interface{}{"tenant_ids": []uint{f.tenantID}}); w.Code != http.StatusBadRequest {
		t.Fatal("issued recipients could be reassigned")
	}
	if w := doJSON(f.env.r, http.MethodPut, path, f.tenant, map[string]interface{}{"tenant_ids": []uint{f.tenantID}}); w.Code != http.StatusForbidden {
		t.Fatal("renter can assign recipients")
	}
}

func TestLandlordOwnershipMigrationPreservesLegacyAndEnforcesNewKey(t *testing.T) {
	env := setupEnv(t)
	room := env.seedRoom("L601", 1000, 0)
	legacy := createBillWithTenant(t, env, room, env.personalLedger, "2026-09", "历史租客")
	if err := env.db.Exec("CREATE UNIQUE INDEX idx_rental_bill_ledger_period ON bills(org_id,room_id,period)").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := database.RunUpgrades(env.db, "0.8.0", []database.Upgrade{{Version: "0.8.0", Name: "landlord_bill_ownership", NoTx: true, Upgrade: migrateBillOwnership}}); err != nil {
			t.Fatal(err)
		}
	}
	var bill Bill
	env.db.First(&bill, legacy)
	if bill.TotalAmount != 1000 || len(bill.RecipientTenantIDs) != 0 || bill.TenancyKey != "" {
		t.Fatal("legacy bill was changed or auto-assigned")
	}
	for _, id := range []uint{101, 102} {
		row := Bill{LedgerID: env.personalLedger, RoomID: room, Period: "2026-10", RoomNo: "L601", RecipientTenantIDs: []uint{id}, TenancyKey: tenantKey([]uint{id})}
		if err := env.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	duplicate := Bill{LedgerID: env.personalLedger, RoomID: room, Period: "2026-10", RoomNo: "L601", TenancyKey: tenantKey([]uint{101})}
	if err := env.db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate tenancy-period accepted by database")
	}
}
