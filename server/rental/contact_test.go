package rental

import (
	"fmt"
	"net/http"
	"smallgo/server/database"
	"strings"
	"testing"
)

func TestTenantContactPendingBoundAndIdentityIsolation(t *testing.T) {
	env := setupEnv(t)
	uid, token := registerTenant(t, env, "wx_contact")
	otherID, otherToken := registerTenant(t, env, "other_contact")
	var before database.User
	env.db.First(&before, uid)
	call := func(tok, phone, expected string, want int) {
		t.Helper()
		w := doJSON(env.r, "PUT", "/api/auth/contact", tok, map[string]interface{}{"phone": phone, "expected_phone": expected, "user_id": otherID})
		if w.Code != want {
			t.Fatalf("contact status=%d want=%d %s", w.Code, want, w.Body.String())
		}
	}
	for _, bad := range []string{"", "123", "1380000900a", "12800009001"} {
		call(token, bad, "", 400)
	}
	call(token, "13800009001", "", 200)
	call(token, "13800009001", "", 200)      // same-payload retry after a lost response
	call(token, "13800009002", "", 409)      // stale editor cannot overwrite
	call(otherToken, "13800009001", "", 200) // shared contact does not merge identity
	_, tenantID := seedTenantWithRoom(t, env, "PHONE-1", "合成联系租客", "2026-01-01", "")
	bindTenantAccount(t, env, env.token, tenantID, uid)
	call(token, "13800009002", "13800009001", 200)
	var after, other database.User
	env.db.First(&after, uid)
	env.db.First(&other, otherID)
	if after.Username != before.Username || after.AuthVersion != before.AuthVersion || after.Password != before.Password || after.APIKey != before.APIKey || after.Phone != "13800009002" || other.Phone != "13800009001" {
		t.Fatal("contact update changed identity or another account")
	}
	var tenancy Tenant
	env.db.First(&tenancy, tenantID)
	if tenancy.AccountUserID != uid || tenancy.Phone != "" {
		t.Fatal("contact edit changed tenancy/archive")
	}
	w := portalGet(env, token, "/api/rental/portal/status")
	data := decodeBody(t, w)
	if w.Code != 200 || field(t, data, "data", "phone") != "13800009002" || field(t, data, "data", "bound") != true {
		t.Fatal("portal did not return fresh own contact")
	}
	call(env.token, "13800009003", "", 403)
	call("", "13800009003", "", 401)
	env.db.Model(&database.User{}).Where("id = ?", uid).Update("status", 0)
	call(token, "13800009003", "13800009002", 401)
}

func TestTenantContactExactSearchDoesNotDisclosePhoneOrBind(t *testing.T) {
	env := setupEnv(t)
	ids := []uint{}
	for i := 0; i < 2; i++ {
		uid, _ := registerTenant(t, env, fmt.Sprintf("wx_phone_%d", i))
		ids = append(ids, uid)
		env.db.Model(&database.User{}).Where("id = ?", uid).Update("phone", "13800009001")
	}
	for _, q := range []string{"1380000", "wx_phone"} {
		w := env.do("GET", "/api/rental/tenant-accounts/candidates?keyword="+q, nil)
		if w.Code != 200 || strings.Contains(w.Body.String(), "13800009001") {
			t.Fatal("partial search disclosed phone")
		}
	}
	w := env.do("GET", "/api/rental/tenant-accounts/candidates?keyword=13800009001", nil)
	rows := field(t, decodeBody(t, w), "data").([]interface{})
	if w.Code != 200 || len(rows) != 2 {
		t.Fatal("full phone must keep duplicate accounts selectable")
	}
	for _, r := range rows {
		if r.(map[string]interface{})["display_name"] != "13800009001" {
			t.Fatal("missing exact-phone label")
		}
	}
	_, tid := seedTenantWithRoom(t, env, "PHONE-2", "另一合成租客", "2026-01-01", "")
	var tr Tenant
	env.db.First(&tr, tid)
	if tr.AccountUserID != 0 {
		t.Fatal("phone search silently bound account")
	}
	bindTenantAccount(t, env, env.token, tid, ids[0])
	w = env.do(http.MethodGet, "/api/rental/tenant-accounts/candidates?keyword=13800009001", nil)
	rows = field(t, decodeBody(t, w), "data").([]interface{})
	if len(rows) != 1 {
		t.Fatal("bound account leaked into candidates")
	}
}
