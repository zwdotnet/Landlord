package rental

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func meterContext(t *testing.T, env *testEnv, room uint, period string) map[string]interface{} {
	t.Helper()
	w := env.do(http.MethodGet, fmt.Sprintf("/api/rental/meter-records/context?room_id=%d&period=%s", room, period), nil)
	if w.Code != 200 {
		t.Fatalf("context %d %s", w.Code, w.Body.String())
	}
	return field(t, decodeBody(t, w), "data").(map[string]interface{})
}

func meterDraft(room uint, period string, water float64, expected interface{}) map[string]interface{} {
	return map[string]interface{}{"room_id": room, "period": period, "water": water, "elec": 200, "gas": 0, "note": "现场抄表", "expected": expected}
}

func TestMeterEntryContextUsesNearestEarlierRecordAndInitials(t *testing.T) {
	env := setupEnv(t)
	room := env.seedRoomWithFees("M-entry", 1000, 10, 100)
	first := meterContext(t, env, room, "2030-03")
	if first["current"] != nil || first["previous"].(map[string]interface{})["water"] != float64(10) || first["previous"].(map[string]interface{})["is_initial"] != true {
		t.Fatal("initial context incorrect")
	}
	for i, p := range []string{"2030-01", "2030-02", "2030-05"} {
		if w := env.do(http.MethodPost, "/api/rental/meter-records", meterDraft(room, p, float64(20+i*10), nil)); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	context := meterContext(t, env, room, "2030-04")
	prev := context["previous"].(map[string]interface{})
	if prev["period"] != "2030-02" || prev["water"] != float64(30) {
		t.Fatal("future reading or gap used incorrectly")
	}
	context = meterContext(t, env, room, "2030-02")
	if context["current"].(map[string]interface{})["water"] != float64(30) || context["previous"].(map[string]interface{})["period"] != "2030-01" {
		t.Fatal("current month included as previous")
	}
	for _, p := range []string{"2030-2", "2030-13", "bad"} {
		if w := env.do(http.MethodGet, fmt.Sprintf("/api/rental/meter-records/context?room_id=%d&period=%s", room, p), nil); w.Code != 400 {
			t.Fatal("invalid period accepted")
		}
	}
}

func TestMeterEntryOptimisticUpdateRetryAndWebCompatibility(t *testing.T) {
	env := setupEnv(t)
	room := env.seedRoom("M-version", 1000, 100)
	post := func(body map[string]interface{}, want int) map[string]interface{} {
		w := env.do(http.MethodPost, "/api/rental/meter-records", body)
		if w.Code != want {
			t.Fatalf("save %d want %d: %s", w.Code, want, w.Body.String())
		}
		if want == 200 {
			return field(t, decodeBody(t, w), "data").(map[string]interface{})
		}
		return nil
	}
	create := meterDraft(room, "2030-01", 20, map[string]interface{}{"id": 0})
	first := post(create, 200)
	repeat := post(create, 200)
	if first["id"] != repeat["id"] || first["updated_at"] != repeat["updated_at"] {
		t.Fatal("identical retry changed record/version")
	}
	version := map[string]interface{}{"id": first["id"], "updated_at": first["updated_at"]}
	second := post(meterDraft(room, "2030-01", 21, version), 200)
	post(meterDraft(room, "2030-01", 22, version), 409)
	post(meterDraft(room, "2030-01", 21, version), 200) // a lost successful response
	post(meterDraft(room, "2030-01", 22, map[string]interface{}{"id": 0}), 409)
	if second["updated_at"] == first["updated_at"] {
		t.Fatal("changed reading kept version")
	}
	post(meterDraft(room, "2030-01", 23, nil), 200) // existing Web callers still overwrite
	if meterContext(t, env, room, "2030-01")["current"].(map[string]interface{})["water"] != float64(23) {
		t.Fatal("Web compatibility lost")
	}
}

func TestMeterEntryConcurrentSubmissionsPreserveOneRecord(t *testing.T) {
	for _, different := range []bool{false, true} {
		t.Run(fmt.Sprint(different), func(t *testing.T) {
			env := setupEnv(t)
			room := env.seedRoom("M-concurrent", 1000, 100)
			start := make(chan struct{})
			codes := make(chan int, 2)
			for i := 0; i < 2; i++ {
				go func(i int) {
					<-start
					value := 20.0
					if different {
						value += float64(i)
					}
					codes <- env.do(http.MethodPost, "/api/rental/meter-records", meterDraft(room, "2030-01", value, map[string]interface{}{"id": 0})).Code
				}(i)
			}
			close(start)
			counts := map[int]int{}
			counts[<-codes]++
			counts[<-codes]++
			if (different && (counts[200] != 1 || counts[409] != 1)) || (!different && counts[200] != 2) {
				t.Fatalf("outcomes %v", counts)
			}
			var n int64
			env.db.Model(&MeterRecord{}).Where("room_id = ?", room).Count(&n)
			if n != 1 {
				t.Fatal("duplicate meter record")
			}
		})
	}
}

func TestMeterEntryMinimalPickerAndModuleIsolation(t *testing.T) {
	env := setupEnv(t)
	owner := newPersonalLedger(t, env, "meter-min-owner")
	shared := createSharedLedger(t, env, owner.token, "抄表共用")
	w := doJSON(env.r, "POST", "/api/rental/rooms?ledger_id="+itoa(shared), owner.token, map[string]interface{}{"room_no": "PICK-M", "community": "search-common", "default_rent": 1234, "initial_water": 8})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	room := uint(field(t, decodeBody(t, w), "data", "id").(float64))
	reader := newPersonalLedger(t, env, "meter-min-reader")
	inviteAndAccept(t, env, owner.token, reader.token, shared, "meter-min-reader")
	grant := func(tier string) {
		w := doJSON(env.r, "PUT", "/api/rental/ledgers/"+itoa(shared)+"/members/"+itoa(reader.userID), owner.token, map[string]interface{}{"perms": map[string]string{ModuleMeters: tier}})
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	grant(PermView)
	base := "?ledger_id=" + itoa(shared)
	if w := doJSON(env.r, "GET", "/api/rental/rooms"+base, reader.token, nil); w.Code != 403 {
		t.Fatal("meter view granted full room view")
	}
	w = doJSON(env.r, "GET", "/api/rental/meter-records/rooms"+base+"&keyword=search-common", reader.token, nil)
	if w.Code != 200 || len(itemsOf(t, w)) != 1 {
		t.Fatal(w.Body.String())
	}
	for _, private := range []string{"default_rent", "current_tenants", "phone", "billing"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("picker leaked room details")
		}
	}
	path := fmt.Sprintf("/api/rental/meter-records/context%s&room_id=%d&period=2030-01", base, room)
	if w := doJSON(env.r, "GET", path, reader.token, nil); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := doJSON(env.r, "POST", "/api/rental/meter-records"+base, reader.token, meterDraft(room, "2030-01", 10, nil)); w.Code != 403 {
		t.Fatal("readonly reader wrote meter")
	}
	grant(PermEdit)
	if w := doJSON(env.r, "POST", "/api/rental/meter-records"+base, reader.token, meterDraft(room, "2030-01", 10, nil)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	foreign := env.seedRoom("search-common-foreign", 1000, 100)
	if w := doJSON(env.r, "GET", fmt.Sprintf("/api/rental/meter-records/context%s&room_id=%d&period=2030-01", base, foreign), reader.token, nil); w.Code != 404 {
		t.Fatal("cross-ledger context leaked")
	}
	w = doJSON(env.r, "GET", "/api/rental/meter-records/rooms"+base+"&keyword=search-common", reader.token, nil)
	if len(itemsOf(t, w)) != 1 {
		t.Fatal("OR search escaped ledger scope")
	}
	if w := doJSON(env.r, "POST", "/api/rental/meter-records"+base, reader.token, meterDraft(foreign, "2030-01", 10, nil)); w.Code != 404 {
		t.Fatal("cross-ledger save allowed")
	}
	if w := doJSON(env.r, "GET", path, env.token, nil); w.Code != 403 {
		t.Fatal("admin bypassed ledger membership")
	}
}

func TestMeterEntryValidationAndWriteFailureRollback(t *testing.T) {
	env := setupEnv(t)
	room := env.seedRoom("M-invalid", 1000, 100)
	for _, patch := range []map[string]interface{}{{"water": -1}, {"note": strings.Repeat("字", 501)}, {"expected": map[string]interface{}{"id": 1}}} {
		body := meterDraft(room, "2030-01", 20, nil)
		for k, v := range patch {
			body[k] = v
		}
		if w := env.do("POST", "/api/rental/meter-records", body); w.Code != 400 {
			t.Fatal("invalid reading accepted")
		}
	}
	body := meterDraft(room, "2030-01", 20, map[string]interface{}{"id": 0})
	if w := env.do("POST", "/api/rental/meter-records", body); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if err := env.db.Exec(`CREATE TRIGGER fail_meter_update BEFORE UPDATE ON meter_records BEGIN SELECT RAISE(ABORT,'injected meter failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	context := meterContext(t, env, room, "2030-01")
	rec := context["current"].(map[string]interface{})
	body = meterDraft(room, "2030-01", 25, map[string]interface{}{"id": rec["id"], "updated_at": rec["updated_at"]})
	if w := env.do("POST", "/api/rental/meter-records", body); w.Code != 500 {
		t.Fatal("injected update succeeded")
	}
	if meterContext(t, env, room, "2030-01")["current"].(map[string]interface{})["water"] != float64(20) {
		t.Fatal("failed update changed reading")
	}
	if err := env.db.Callback().Query().Before("gorm:query").Register("test:meter_lookup_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "meter_records" {
			tx.AddError(errors.New("injected lookup failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer env.db.Callback().Query().Remove("test:meter_lookup_failure")
	if w := env.do("POST", "/api/rental/meter-records", meterDraft(room, "2030-02", 30, nil)); w.Code != 500 {
		t.Fatal("lookup failure treated as missing record")
	}
}

func TestMeterEntryEditingAfterIssueKeepsOriginalBill(t *testing.T) {
	env, room, tenant := documentFixture(t)
	plan := saveTestPlan(t, env, room, tenant, nil)
	autoRun(t, env, "2030-01-07 09:00")
	if taskFor(t, env, plan.ID, "2030-01").Status != "waiting_meter" {
		t.Fatal("missing reading did not wait")
	}
	body := meterDraft(room, "2030-01", 12, map[string]interface{}{"id": 0})
	body["elec"] = 100
	if w := env.do("POST", "/api/rental/meter-records", body); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	autoRun(t, env, "2030-01-07 09:02")
	autoRun(t, env, "2030-01-07 09:03")
	task := taskFor(t, env, plan.ID, "2030-01")
	if task.Status != "issued" {
		t.Fatal("confirmed reading did not resume automation")
	}
	var original Bill
	env.db.First(&original, task.BillID)
	context := meterContext(t, env, room, "2030-01")
	if context["bill_exists"] != true {
		t.Fatal("issued bill warning missing")
	}
	rec := context["current"].(map[string]interface{})
	if w := env.do("POST", "/api/rental/meter-records", meterDraft(room, "2030-01", 22, map[string]interface{}{"id": rec["id"], "updated_at": rec["updated_at"]})); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	autoRun(t, env, "2030-01-07 09:04")
	var kept Bill
	env.db.First(&kept, task.BillID)
	var count int64
	env.db.Model(&Bill{}).Count(&count)
	if kept.WaterFee != original.WaterFee || kept.TotalAmount != original.TotalAmount || count != 1 {
		t.Fatal("meter update changed/duplicated issued bill")
	}
}
