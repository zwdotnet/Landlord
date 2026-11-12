package user

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"smallgo/server/auth"
	"smallgo/server/database"
	"smallgo/server/sysconfig"

	"github.com/gin-gonic/gin"
)

type wechatRoundTrip func(*http.Request) (*http.Response, error)

func (f wechatRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWeChatLoginRequiresPhoneOnceAndNeverAutoClaimsTenant(t *testing.T) {
	db := setupUserDB(t)
	owner := createTestUser(t, db, "owner", "test-password")
	_ = owner
	if err := sysconfig.UpdateConfig(db, tenantPortalConfigKey, "true", 0); err != nil {
		t.Fatal(err)
	}
	if err := sysconfig.UpdateConfig(db, "jwt_secret", testJWTSecret, 0); err != nil {
		t.Fatal(err)
	}
	client := wechatClient{appID: "wx-test-app", secret: "secret-not-returned", http: &http.Client{Transport: wechatRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.weixin.qq.com" || req.URL.Path != "/sns/jscode2session" || req.URL.Query().Get("grant_type") != "authorization_code" {
			t.Fatal("wrong WeChat contract")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"openid":"isolated-openid","session_key":"must-not-leak"}`))}, nil
	})}}
	r := gin.New()
	r.POST("/auth/wechat/login", wechatLoginHandler(db, client))
	post := func(phone string) map[string]interface{} {
		w := httptest.NewRecorder()
		body, _ := json.Marshal(map[string]string{"code": "one-use-code", "phone": phone})
		r.ServeHTTP(w, jsonRequest(http.MethodPost, "/auth/wechat/login", string(body)))
		if w.Code != 200 {
			t.Fatalf("login status %d: %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "must-not-leak") || strings.Contains(w.Body.String(), "isolated-openid") {
			t.Fatal("WeChat credentials leaked")
		}
		var result struct {
			Data map[string]interface{} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		return result.Data
	}
	if !post("")["phone_required"].(bool) {
		t.Fatal("first login did not request contact phone")
	}
	var count int64
	db.Model(&database.User{}).Count(&count)
	if count != 1 {
		t.Fatal("phone prompt created an account")
	}
	first := post("138" + "0000" + "9001")
	u := first["user"].(map[string]interface{})
	if u["role"] != RoleTenant || u["status"].(float64) != 2 {
		t.Fatalf("first login must be pending tenant: %v", u)
	}
	if _, _, _, _, err := auth.ParseToken(first["token"].(string), testJWTSecret); err != nil {
		t.Fatal(err)
	}
	repeat := post("")
	if repeat["user"].(map[string]interface{})["id"] != u["id"] {
		t.Fatal("repeat login created another account")
	}
	var pending database.User
	db.First(&pending, uint(u["id"].(float64)))
	if pending.Phone != "138"+"0000"+"9001" || pending.Role != RoleTenant || pending.Status != 2 {
		t.Fatal("manual phone must not activate account")
	}
	db.Model(&database.User{}).Where("id = ?", pending.ID).Update("status", 1)
	if post("")["user"].(map[string]interface{})["status"].(float64) != 1 {
		t.Fatal("landlord approval not reflected at next login")
	}
	db.Model(&database.User{}).Where("id = ?", pending.ID).Update("status", 0)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, jsonRequest("POST", "/auth/wechat/login", `{"code":"valid-code"}`))
	if w.Code != 403 {
		t.Fatal("disabled account logged in")
	}
	db.Model(&database.User{}).Count(&count)
	if count != 2 {
		t.Fatalf("duplicate accounts: %d", count)
	}
}

func TestWeChatExchangeFailureCannotLeakSecretsOrCreateUser(t *testing.T) {
	db := setupUserDB(t)
	createTestUser(t, db, "owner", "test-password")
	_ = sysconfig.UpdateConfig(db, tenantPortalConfigKey, "true", 0)
	client := wechatClient{appID: "test", secret: "private-app-secret", http: &http.Client{Transport: wechatRoundTrip(func(req *http.Request) (*http.Response, error) { return nil, errors.New(req.URL.String()) })}}
	r := gin.New()
	r.POST("/login", wechatLoginHandler(db, client))
	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"code": "private-code", "phone": "138" + "0000" + "9001"})
	r.ServeHTTP(w, jsonRequest("POST", "/login", string(body)))
	if w.Code != 400 || strings.Contains(w.Body.String(), "private-") {
		t.Fatalf("unsafe error: %s", w.Body.String())
	}
	var count int64
	db.Model(&database.User{}).Count(&count)
	if count != 1 {
		t.Fatal("failed exchange created account")
	}
}

func TestWeChatCannotInitializePlatform(t *testing.T) {
	db := setupUserDB(t)
	r := gin.New()
	r.POST("/login", wechatLoginHandler(db, wechatClient{appID: "test", secret: "test"}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, jsonRequest("POST", "/login", `{"code":"code"}`))
	if w.Code != 403 {
		t.Fatal("WeChat login must not initialize the platform")
	}
}
