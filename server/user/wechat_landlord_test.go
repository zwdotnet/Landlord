package user

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"smallgo/server/auth"
	"smallgo/server/database"
	"smallgo/server/middleware"
	"smallgo/server/sysconfig"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ownerWeChatFixture struct {
	db     *gorm.DB
	r      *gin.Engine
	client wechatClient
	codes  atomic.Int64
}

func newOwnerWeChatFixture(t *testing.T) *ownerWeChatFixture {
	t.Helper()
	f := &ownerWeChatFixture{db: setupUserDB(t)}
	if err := sysconfig.UpdateConfig(f.db, "jwt_secret", testJWTSecret, 0); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	used := map[string]bool{}
	f.client = wechatClient{appID: "wx-owner-test", secret: "private-owner-secret", http: &http.Client{Transport: wechatRoundTrip(func(req *http.Request) (*http.Response, error) {
		code := req.URL.Query().Get("js_code")
		mu.Lock()
		defer mu.Unlock()
		data := `{"errcode":40029}`
		if !used[code] {
			used[code] = true
			raw, _ := json.Marshal(map[string]string{"openid": strings.Split(code, ":")[0], "session_key": "private-session"})
			data = string(raw)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(data))}, nil
	})}}
	f.routes()
	return f
}

func (f *ownerWeChatFixture) routes() {
	f.r = gin.New()
	f.r.POST("/login", landlordWeChatLoginHandler(f.db, f.client))
	f.r.POST("/tenant", wechatLoginHandler(f.db, f.client))
	g := f.r.Group("", middleware.RequireAuth(testJWTSecret, f.db))
	g.GET("/bind", landlordWeChatBindingHandler(f.db, f.client, "status"))
	g.POST("/bind", landlordWeChatBindingHandler(f.db, f.client, "bind"))
	g.DELETE("/bind", landlordWeChatBindingHandler(f.db, f.client, "unbind"))
}

func (f *ownerWeChatFixture) token(t *testing.T, u database.User) string {
	t.Helper()
	v, err := auth.GenerateToken(u.ID, u.Username, u.Role, u.AuthVersion, testJWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (f *ownerWeChatFixture) call(method, path, token string, body map[string]string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := jsonRequest(method, path, string(raw))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return perform(f.r, req)
}

func (f *ownerWeChatFixture) bind(token, identity, password string) *httptest.ResponseRecorder {
	return f.call("POST", "/bind", token, map[string]string{"code": fmt.Sprintf("%s:%d", identity, f.codes.Add(1)), "password": password})
}

func ownerData(t *testing.T, w *httptest.ResponseRecorder, want int) map[string]interface{} {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" && want != 401 {
		t.Fatal("missing no-store")
	}
	for _, private := range []string{"private-owner-secret", "private-openid", "private-session", "test-password"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("credential in response")
		}
	}
	var envelope struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func TestWeChatLandlordBindingLoginAndUnbindRevokesEveryJWT(t *testing.T) {
	f := newOwnerWeChatFixture(t)
	u := createTestUser(t, f.db, "owner", "test-password")
	token := f.token(t, u)
	if ownerData(t, f.call("GET", "/bind", token, nil), 200)["bound"] != false {
		t.Fatal("unexpected binding")
	}
	ownerData(t, f.bind(token, "private-openid", "incorrect"), 403)
	ownerData(t, f.bind(token, "private-openid", "test-password"), 200)
	ownerData(t, f.bind(token, "private-openid", "test-password"), 200) // idempotent, fresh code
	// Tenant portal defaults to disabled; landlord login must still succeed.
	login := ownerData(t, f.call("POST", "/login", "", map[string]string{"code": "private-openid:login"}), 200)
	if login["user"].(map[string]interface{})["id"] != float64(u.ID) {
		t.Fatal("wrong account")
	}
	quickToken := login["token"].(string)
	ownerData(t, f.call("DELETE", "/bind", token, map[string]string{"password": "incorrect"}), 403)
	if ownerData(t, f.call("GET", "/bind", quickToken, nil), 200)["bound"] != true {
		t.Fatal("wrong password removed binding")
	}
	if ownerData(t, f.call("DELETE", "/bind", token, map[string]string{"password": "test-password"}), 200)["requires_login"] != true {
		t.Fatal("missing logout instruction")
	}
	for _, old := range []string{token, quickToken} {
		ownerData(t, f.call("GET", "/bind", old, nil), 401)
	}
	ownerData(t, f.call("POST", "/login", "", map[string]string{"code": "private-openid:after"}), 403)
	f.db.First(&u, u.ID)
	if u.AuthVersion != 2 {
		t.Fatal("all JWT versions were not revoked")
	}
	if ownerData(t, f.call("DELETE", "/bind", f.token(t, u), map[string]string{"password": "test-password"}), 200)["requires_login"] != false {
		t.Fatal("empty unbind not idempotent")
	}
	var count int64
	f.db.Model(&database.User{}).Count(&count)
	if count != 1 {
		t.Fatal("created account")
	}
}

func TestWeChatLandlordConflictsNeverReplaceTenantOrOtherAccount(t *testing.T) {
	f := newOwnerWeChatFixture(t)
	a := createTestUser(t, f.db, "a", "test-password")
	b := createTestUser(t, f.db, "b", "test-password")
	ownerData(t, f.bind(f.token(t, a), "private-openid", "test-password"), 200)
	ownerData(t, f.bind(f.token(t, b), "private-openid", "test-password"), 409)
	ownerData(t, f.bind(f.token(t, a), "other-openid", "test-password"), 409)
	tenant := createTestUser(t, f.db, "tenant", "test-password")
	f.db.Model(&tenant).Update("role", RoleTenant)
	tenant.Role = RoleTenant
	f.db.Create(&WeChatIdentity{AppID: f.client.appID, OpenID: "tenant-openid", UserID: tenant.ID})
	ownerData(t, f.bind(f.token(t, b), "tenant-openid", "test-password"), 409)
	for _, method := range []string{"GET", "POST", "DELETE"} {
		ownerData(t, f.call(method, "/bind", f.token(t, tenant), map[string]string{"password": "test-password", "code": "new:1"}), 403)
	}
	ownerData(t, f.call("POST", "/login", "", map[string]string{"code": "tenant-openid:login"}), 403)
	var identities []WeChatIdentity
	f.db.Order("user_id").Find(&identities)
	if len(identities) != 2 || identities[0].UserID != a.ID || identities[0].OpenID != "private-openid" || identities[1].UserID != tenant.ID {
		t.Fatal("original mapping changed")
	}
	f.db.First(&tenant, tenant.ID)
	if tenant.Role != RoleTenant {
		t.Fatal("tenant promoted")
	}
}

func TestWeChatLandlordLifecycleAndPortalIndependence(t *testing.T) {
	for _, role := range []string{RoleUser, RoleAdmin} {
		t.Run(role, func(t *testing.T) {
			f := newOwnerWeChatFixture(t)
			u := createTestUser(t, f.db, "owner", "test-password")
			f.db.Model(&u).Update("role", role)
			u.Role = role
			ownerData(t, f.bind(f.token(t, u), "private-openid", "test-password"), 200)
			f.db.Model(&u).Update("status", 2)
			u.Status = 2
			login := ownerData(t, f.call("POST", "/login", "", map[string]string{"code": "private-openid:pending"}), 200)
			if login["user"].(map[string]interface{})["status"] != float64(2) {
				t.Fatal("pending status lost")
			}
			ownerData(t, f.bind(f.token(t, u), "private-openid", "test-password"), 403)
			ownerData(t, f.call("GET", "/bind", f.token(t, u), nil), 200)
			f.db.Model(&u).Update("status", 0)
			ownerData(t, f.call("POST", "/login", "", map[string]string{"code": "private-openid:disabled"}), 403)
			ownerData(t, f.call("DELETE", "/bind", f.token(t, u), map[string]string{"password": "test-password"}), 401)
			f.db.Model(&u).Update("status", 2)
			ownerData(t, f.call("DELETE", "/bind", f.token(t, u), map[string]string{"password": "test-password"}), 200)
		})
	}
}

func TestWeChatLandlordUnknownInvalidCodeAndMissingConfig(t *testing.T) {
	f := newOwnerWeChatFixture(t)
	u := createTestUser(t, f.db, "owner", "test-password")
	token := f.token(t, u)
	ownerData(t, f.call("POST", "/login", "", map[string]string{"code": "private-openid:unknown"}), 403)
	ownerData(t, f.call("POST", "/login", "", map[string]string{"code": "private-openid:unknown"}), 400)
	ownerData(t, f.call("POST", "/bind", token, map[string]string{"code": "private-openid:unknown", "password": "test-password"}), 400)
	for _, code := range []string{"", strings.Repeat("x", 513)} {
		ownerData(t, f.call("POST", "/login", "", map[string]string{"code": code}), 400)
	}
	ownerData(t, f.call("POST", "/bind", "", map[string]string{"code": "x:1", "password": "test-password"}), 401)
	f.client.secret = ""
	f.routes()
	if ownerData(t, f.call("GET", "/bind", token, nil), 200)["available"] != false {
		t.Fatal("incorrect availability")
	}
	ownerData(t, f.call("POST", "/login", "", map[string]string{"code": "x:2"}), 503)
	ownerData(t, f.bind(token, "x", "test-password"), 503)
	var count int64
	f.db.Model(&WeChatIdentity{}).Count(&count)
	if count != 0 {
		t.Fatal("failed call changed bindings")
	}
}

func TestWeChatLandlordConcurrentClaimsHaveOneWinner(t *testing.T) {
	for _, sameAccount := range []bool{false, true} {
		t.Run(fmt.Sprint(sameAccount), func(t *testing.T) {
			f := newOwnerWeChatFixture(t)
			a := createTestUser(t, f.db, "a", "test-password")
			b := createTestUser(t, f.db, "b", "test-password")
			if sameAccount {
				b = a
			}
			tokens := []string{f.token(t, a), f.token(t, b)}
			start := make(chan struct{})
			results := make(chan int, 2)
			for i := 0; i < 2; i++ {
				go func(i int) {
					<-start
					identity := "shared-openid"
					if sameAccount {
						identity = fmt.Sprintf("different-openid-%d", i)
					}
					results <- f.bind(tokens[i], identity, "test-password").Code
				}(i)
			}
			close(start)
			counts := map[int]int{}
			counts[<-results]++
			counts[<-results]++
			if counts[200] != 1 || counts[409] != 1 {
				t.Fatalf("claim outcomes %v", counts)
			}
			var count int64
			f.db.Model(&WeChatIdentity{}).Count(&count)
			if count != 1 {
				t.Fatal("multiple claims")
			}
		})
	}
}

func TestWeChatLandlordRejectsPasswordChangeDuringExchange(t *testing.T) {
	f := newOwnerWeChatFixture(t)
	u := createTestUser(t, f.db, "owner", "test-password")
	token := f.token(t, u)
	base := f.client.http.Transport
	f.client.http = &http.Client{Transport: wechatRoundTrip(func(req *http.Request) (*http.Response, error) {
		if err := ChangePassword(f.db, u.ID, "test-password", "", "new-password"); err != nil {
			t.Fatal(err)
		}
		return base.RoundTrip(req)
	})}
	f.routes()
	ownerData(t, f.bind(token, "private-openid", "test-password"), 409)
	var count int64
	f.db.Model(&WeChatIdentity{}).Count(&count)
	if count != 0 {
		t.Fatal("stale password bound account")
	}
}

func TestWeChatLandlordTenantEndpointKeepsOriginalMapping(t *testing.T) {
	f := newOwnerWeChatFixture(t)
	u := createTestUser(t, f.db, "owner", "test-password")
	ownerData(t, f.bind(f.token(t, u), "private-openid", "test-password"), 200)
	_ = sysconfig.UpdateConfig(f.db, tenantPortalConfigKey, "true", 0)
	ownerData(t, f.call("POST", "/tenant", "", map[string]string{"code": "private-openid:tenant"}), 403)
	first := ownerData(t, f.call("POST", "/tenant", "", map[string]string{"code": "new-tenant:1", "phone": "138" + "0000" + "9001"}), 200)
	repeat := ownerData(t, f.call("POST", "/tenant", "", map[string]string{"code": "new-tenant:2"}), 200)
	if first["user"].(map[string]interface{})["id"] != repeat["user"].(map[string]interface{})["id"] {
		t.Fatal("tenant duplicated")
	}
	if first["user"].(map[string]interface{})["role"] != RoleTenant {
		t.Fatal("wrong tenant role")
	}
}

func TestWeChatLandlordTransportErrorRedactsCredentials(t *testing.T) {
	f := newOwnerWeChatFixture(t)
	u := createTestUser(t, f.db, "owner", "test-password")
	f.client.http = &http.Client{Transport: wechatRoundTrip(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("transport: %s", req.URL.String())
	})}
	f.routes()
	ownerData(t, f.bind(f.token(t, u), "private-openid", "test-password"), 400)
	ownerData(t, f.call("POST", "/login", "", map[string]string{"code": "private-owner-secret"}), 400)
}

func TestWeChatLandlordLoginSnapshotCannotOutliveUnbind(t *testing.T) {
	f := newOwnerWeChatFixture(t)
	u := createTestUser(t, f.db, "owner", "test-password")
	ownerData(t, f.bind(f.token(t, u), "private-openid", "test-password"), 200)
	read, resume := make(chan struct{}), make(chan struct{})
	if err := f.db.Callback().Query().After("gorm:query").Register("test:pause_wechat_snapshot", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" && strings.Contains(tx.Statement.SQL.String(), "SELECT `user_id`") {
			close(read)
			<-resume
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Query().Remove("test:pause_wechat_snapshot")
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- f.call("POST", "/login", "", map[string]string{"code": "private-openid:racing"}) }()
	<-read
	ownerData(t, f.call("DELETE", "/bind", f.token(t, u), map[string]string{"password": "test-password"}), 200)
	close(resume)
	login := ownerData(t, <-result, 200)
	ownerData(t, f.call("GET", "/bind", login["token"].(string), nil), 401)
}

func TestWeChatLandlordOtherAppConflictAndUnconfiguredUnbind(t *testing.T) {
	f := newOwnerWeChatFixture(t)
	u := createTestUser(t, f.db, "owner", "test-password")
	f.db.Create(&WeChatIdentity{AppID: "other-app", OpenID: "private-openid", UserID: u.ID})
	status := ownerData(t, f.call("GET", "/bind", f.token(t, u), nil), 200)
	if status["bound"] != true || status["current_app"] != false {
		t.Fatal("other app binding hidden")
	}
	ownerData(t, f.bind(f.token(t, u), "new-openid", "test-password"), 409)
	f.client = wechatClient{}
	f.routes()
	ownerData(t, f.call("DELETE", "/bind", f.token(t, u), map[string]string{"password": "test-password"}), 200)
}

func TestWeChatLandlordUnbindFailureRollsBackIdentityAndTokenVersion(t *testing.T) {
	f := newOwnerWeChatFixture(t)
	u := createTestUser(t, f.db, "owner", "test-password")
	token := f.token(t, u)
	ownerData(t, f.bind(token, "private-openid", "test-password"), 200)
	if err := f.db.Exec(`CREATE TRIGGER fail_wechat_revoke BEFORE UPDATE OF auth_version ON users
		WHEN NEW.auth_version != OLD.auth_version BEGIN SELECT RAISE(ABORT, 'injected revoke failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	ownerData(t, f.call("DELETE", "/bind", token, map[string]string{"password": "test-password"}), 500)
	if ownerData(t, f.call("GET", "/bind", token, nil), 200)["bound"] != true {
		t.Fatal("failed revoke partially removed identity")
	}
	f.db.First(&u, u.ID)
	if u.AuthVersion != 1 {
		t.Fatal("failed revoke changed JWT version")
	}
}
