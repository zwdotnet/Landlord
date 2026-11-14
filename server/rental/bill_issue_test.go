package rental

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"smallgo/server/sysconfig"
)

func issueFixture(t *testing.T) (*testEnv, uint, uint) {
	t.Helper()
	env := setupEnv(t)
	w := env.do("POST", "/api/rental/rooms", map[string]interface{}{"room_no": "ISSUE-01", "default_rent": 1000, "initial_water": 10, "initial_elec": 100, "elec_price": 1, "gas_price": 1})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	room := uint(num(field(t, decodeBody(t, w), "data", "id")))
	w = env.do("POST", "/api/rental/tenants", map[string]interface{}{"room_id": room, "name": "合成同名租客", "move_in_date": "2029-01-01", "lease_end_date": "2031-12-31", "deposit": 200, "water_price": 5})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	tenant := uint(num(field(t, decodeBody(t, w), "data", "id")))
	w = env.do("POST", "/api/rental/meter-records", map[string]interface{}{"room_id": room, "period": "2030-01", "water": 20, "elec": 200, "gas": 0})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	return env, room, tenant
}

func issuePreview(t *testing.T, env *testEnv, room, tenant uint) map[string]interface{} {
	t.Helper()
	w := env.do("GET", fmt.Sprintf("/api/rental/bills/issue-preview?room_id=%d&period=2030-01&tenant_ids=%d", room, tenant), nil)
	if w.Code != 200 {
		t.Fatalf("preview %d %s", w.Code, w.Body.String())
	}
	return field(t, decodeBody(t, w), "data").(map[string]interface{})
}

func confirmIssue(env *testEnv, token string) *httptest.ResponseRecorder {
	return env.do("POST", "/api/rental/bills/confirm", map[string]interface{}{"confirmation_token": token})
}

func TestBillIssuePreviewConfirmationMatchesFeesAndNeverDuplicates(t *testing.T) {
	env, room, tenant := issueFixture(t)
	seedCustomFee(t, env, "宽带费", 30)
	view := issuePreview(t, env, room, tenant)
	if view["can_confirm"] != true || num(view["bill"].(map[string]interface{})["payable"]) != 1380 {
		t.Fatal("preview calculation mismatch")
	}
	var n int64
	env.db.Model(&Bill{}).Count(&n)
	if n != 0 {
		t.Fatal("preview wrote a bill")
	}
	token := view["confirmation_token"].(string)
	w := confirmIssue(env, token)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	created := field(t, decodeBody(t, w), "data").(map[string]interface{})
	bill := created["bill"].(map[string]interface{})
	if created["created"] != true || num(bill["payable"]) != 1380 {
		t.Fatal("confirmed amount differs")
	}
	w = confirmIssue(env, token)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if field(t, decodeBody(t, w), "data", "created") != false || field(t, decodeBody(t, w), "data", "bill", "id") != bill["id"] {
		t.Fatal("retry did not preserve original")
	}
	env.db.Model(&Bill{}).Count(&n)
	if n != 1 {
		t.Fatal("duplicate bill")
	}
	if issuePreview(t, env, room, tenant)["exists"] != true {
		t.Fatal("existing preview not shown")
	}
}

func TestBillIssueDataChangesInvalidatePreview(t *testing.T) {
	for _, kind := range []string{"rent", "meter", "tenant", "custom", "carry"} {
		t.Run(kind, func(t *testing.T) {
			env, room, tenant := issueFixture(t)
			if kind == "carry" {
				if err := setLedgerConfigValue(env.db, carryoverConfigKey, "true", env.personalLedger); err != nil {
					t.Fatal(err)
				}
				createBillFor(t, env, room, "2029-12")
			}
			token := issuePreview(t, env, room, tenant)["confirmation_token"].(string)
			switch kind {
			case "rent":
				env.db.Model(&Room{}).Where("id = ?", room).Update("default_rent", 1200)
			case "meter":
				env.db.Model(&MeterRecord{}).Where("room_id = ? AND period = ?", room, "2030-01").Update("water", 25)
			case "tenant":
				env.db.Model(&Tenant{}).Where("id = ?", tenant).Update("name", "另一个显示名")
			case "custom":
				seedCustomFee(t, env, "停车费", 40)
			case "carry":
				env.db.Model(&Bill{}).Where("room_id = ? AND period = ?", room, "2029-12").Update("paid_amount", 500)
			}
			if w := confirmIssue(env, token); w.Code != 409 {
				t.Fatalf("changed data accepted %d %s", w.Code, w.Body.String())
			}
			var n int64
			env.db.Model(&Bill{}).Where("period = ?", "2030-01").Count(&n)
			if n != 0 {
				t.Fatal("stale preview created bill")
			}
		})
	}
}

func TestBillIssueTokenExpiryScopeAndNoClientPriceOverride(t *testing.T) {
	env, room, tenant := issueFixture(t)
	view := issuePreview(t, env, room, tenant)
	token := view["confirmation_token"].(string)
	if w := confirmIssue(env, token+"tamper"); w.Code != 409 {
		t.Fatal("tampered preview accepted")
	}
	secret, _ := sysconfig.GetConfig(env.db, "jwt_secret", 0)
	claims, err := parseIssueToken(token, secret)
	if err != nil {
		t.Fatal(err)
	}
	claims.Expires = time.Now().Add(-time.Second).Unix()
	expired, _ := issueToken(claims, secret)
	if w := confirmIssue(env, expired); w.Code != 409 {
		t.Fatal("expired preview accepted")
	}
	other := newPersonalLedger(t, env, "issue-other")
	if w := doJSON(env.r, "POST", "/api/rental/bills/confirm?ledger_id="+itoa(other.ledgerID), other.token, map[string]interface{}{"confirmation_token": token}); w.Code != 403 {
		t.Fatal("other account reused preview")
	}
	w := env.do("POST", "/api/rental/bills/confirm", map[string]interface{}{"confirmation_token": token, "rent": 0, "water_fee": 0, "tenant_ids": []uint{9999}})
	if w.Code != 200 || num(field(t, decodeBody(t, w), "data", "bill", "payable")) != 1350 {
		t.Fatal("client prices/IDs changed confirmation")
	}
}

func TestBillIssueConcurrentManualAndAutomationCreateOneBill(t *testing.T) {
	for _, automated := range []bool{false, true} {
		t.Run(fmt.Sprint(automated), func(t *testing.T) {
			env, room, tenant := issueFixture(t)
			if automated {
				saveTestPlan(t, env, room, tenant, nil)
			}
			token := issuePreview(t, env, room, tenant)["confirmation_token"].(string)
			start := make(chan struct{})
			done := make(chan error, 2)
			go func() {
				<-start
				w := confirmIssue(env, token)
				if w.Code != 200 {
					done <- fmt.Errorf("manual status %d", w.Code)
				} else {
					done <- nil
				}
			}()
			go func() {
				<-start
				if automated {
					now, _ := time.ParseInLocation("2006-01-02 15:04", "2030-01-07 09:00", businessZone)
					done <- runAutomation(env.db, now, nil)
				} else {
					w := confirmIssue(env, token)
					if w.Code != 200 {
						done <- fmt.Errorf("repeat status %d", w.Code)
					} else {
						done <- nil
					}
				}
			}()
			close(start)
			for i := 0; i < 2; i++ {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			var n int64
			env.db.Model(&Bill{}).Count(&n)
			if n != 1 {
				t.Fatalf("bills %d", n)
			}
		})
	}
}

func TestBillIssueBlockedDraftsAndOverlapPreserveOriginal(t *testing.T) {
	env, room, tenant := issueFixture(t)
	env.db.Where("room_id = ?", room).Delete(&MeterRecord{})
	view := issuePreview(t, env, room, tenant)
	if view["can_confirm"] != false || view["confirmation_token"] != nil {
		t.Fatal("missing meter was confirmable")
	}
	env.do("POST", "/api/rental/meter-records", map[string]interface{}{"room_id": room, "period": "2030-01", "water": 20, "elec": 200})
	first := createBillFor(t, env, room, "2030-01")
	w := env.do("POST", "/api/rental/tenants", map[string]interface{}{"room_id": room, "name": "同住人", "move_in_date": "2029-01-01", "lease_end_date": "2031-12-31"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	second := uint(num(field(t, decodeBody(t, w), "data", "id")))
	w = env.do("GET", fmt.Sprintf("/api/rental/bills/issue-preview?room_id=%d&period=2030-01&tenant_ids=%d,%d", room, tenant, second), nil)
	if w.Code != 200 || field(t, decodeBody(t, w), "data", "exists") != true || field(t, decodeBody(t, w), "data", "existing_match") != false || field(t, decodeBody(t, w), "data", "bill", "id") != first["id"] {
		t.Fatal("overlapping tenancy was newly billable")
	}
}

func TestBillIssueSameNameTurnoverSeparatesStayIDs(t *testing.T) {
	env, room, old := issueFixture(t)
	env.db.Model(&Tenant{}).Where("id = ?", old).Updates(map[string]interface{}{"active": false, "move_out_date": "2030-01-15"})
	w := env.do("POST", "/api/rental/tenants", map[string]interface{}{"room_id": room, "name": "合成同名租客", "move_in_date": "2030-01-16", "lease_end_date": "2031-12-31"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	next := uint(num(field(t, decodeBody(t, w), "data", "id")))
	if issuePreview(t, env, room, next)["can_confirm"] != false {
		t.Fatal("missing predecessor settlement guessed")
	}
	w = env.do("POST", "/api/rental/bills", map[string]interface{}{"room_id": room, "period": "2030-01", "tenant_ids": []uint{old}, "water_now": 15, "elec_now": 150})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	view := issuePreview(t, env, room, next)
	bill := view["bill"].(map[string]interface{})
	if view["can_confirm"] != true || num(bill["water_last"]) != 15 || num(bill["elec_last"]) != 150 {
		t.Fatal("new tenancy boundary incorrect")
	}
	if w := confirmIssue(env, view["confirmation_token"].(string)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	oldView := issuePreview(t, env, room, old)
	if oldView["exists"] != true || oldView["bill"].(map[string]interface{})["tenancy_key"] == bill["tenancy_key"] {
		t.Fatal("same name merged stays")
	}
	var n int64
	env.db.Model(&Bill{}).Count(&n)
	if n != 2 {
		t.Fatal("turnover bills count")
	}
}

func TestBillIssuePermissionsAndMinimalCandidates(t *testing.T) {
	env, room, tenant := issueFixture(t)
	owner := newPersonalLedger(t, env, "issue-owner")
	reader := newPersonalLedger(t, env, "issue-reader")
	shared := createSharedLedger(t, env, owner.token, "账单共用")
	inviteAndAccept(t, env, owner.token, reader.token, shared, "issue-reader")
	env.db.Model(&Room{}).Where("id = ?", room).Update("org_id", shared)
	env.db.Model(&Tenant{}).Where("id = ?", tenant).Update("org_id", shared)
	env.db.Model(&MeterRecord{}).Where("room_id = ?", room).Update("org_id", shared)
	w := doJSON(env.r, "PUT", "/api/rental/ledgers/"+itoa(shared)+"/members/"+itoa(reader.userID), owner.token, map[string]interface{}{"perms": map[string]string{ModuleBills: PermView}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	base := "?ledger_id=" + itoa(shared)
	w = doJSON(env.r, "GET", "/api/rental/bills/issue-rooms"+base, reader.token, nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, private := range []string{"default_rent", "current_tenants", "phone"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("picker leaked full room")
		}
	}
	w = doJSON(env.r, "GET", fmt.Sprintf("/api/rental/bills/issue-tenants%s&room_id=%d&period=2030-01", base, room), reader.token, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "phone") || strings.Contains(w.Body.String(), "deposit") {
		t.Fatal("candidate data exceeded billing selection")
	}
	w = doJSON(env.r, "GET", fmt.Sprintf("/api/rental/bills/issue-preview%s&room_id=%d&period=2030-01&tenant_ids=%d", base, room, tenant), reader.token, nil)
	if w.Code != 200 || field(t, decodeBody(t, w), "data", "can_confirm") != false {
		t.Fatal("readonly confirmable")
	}
	if w := doJSON(env.r, "POST", "/api/rental/bills/confirm"+base, reader.token, map[string]string{"confirmation_token": "invalid"}); w.Code != 403 {
		t.Fatal("readonly write allowed")
	}
	if w := doJSON(env.r, "GET", "/api/rental/bills/issue-rooms"+base, env.token, nil); w.Code != 403 {
		t.Fatal("admin bypassed ledger")
	}
}

func TestBillIssueFailedItemsRollBackEntireBill(t *testing.T) {
	env, room, tenant := issueFixture(t)
	token := issuePreview(t, env, room, tenant)["confirmation_token"].(string)
	if err := env.db.Exec(`CREATE TRIGGER fail_issue_items BEFORE INSERT ON bill_items BEGIN SELECT RAISE(ABORT,'injected issue failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if w := confirmIssue(env, token); w.Code != 500 {
		t.Fatal("injected failure succeeded")
	}
	var n int64
	env.db.Model(&Bill{}).Count(&n)
	if n != 0 {
		t.Fatal("partial bill left behind")
	}
	env.db.Exec("DROP TRIGGER fail_issue_items")
	if w := confirmIssue(env, token); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}

func TestBillIssueUnsupportedDraftsCannotReceiveConfirmation(t *testing.T) {
	for _, kind := range []string{"move-in", "departed", "low-reading", "generated-reading", "rent-review"} {
		t.Run(kind, func(t *testing.T) {
			env, room, tenant := issueFixture(t)
			switch kind {
			case "move-in":
				env.db.Model(&Tenant{}).Where("id = ?", tenant).Update("move_in_date", "")
			case "departed":
				env.db.Model(&Tenant{}).Where("id = ?", tenant).Updates(map[string]interface{}{"active": false, "move_out_date": "2030-01-15"})
			case "low-reading":
				env.db.Model(&MeterRecord{}).Where("room_id = ?", room).Update("water", 9)
			case "generated-reading":
				env.db.Model(&MeterRecord{}).Where("room_id = ?", room).Update("note", "随账单生成")
			case "rent-review":
				env.db.Create(&LeaseRenewal{LedgerID: env.personalLedger, RoomID: room, TenantID: tenant, RentBefore: 1000, RentAfter: 1200})
			}
			view := issuePreview(t, env, room, tenant)
			if view["can_confirm"] != false || view["confirmation_token"] != nil || view["block_reason"] == "" {
				t.Fatal("unsafe draft received confirmation permission")
			}
			var n int64
			env.db.Model(&Bill{}).Count(&n)
			if n != 0 {
				t.Fatal("blocked preview created a bill")
			}
		})
	}
}
