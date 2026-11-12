package audit

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"smallgo/server/database"
	"smallgo/server/sysconfig"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupAuditDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := database.InitDB(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB(db) })
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := sysconfig.InitDefaultConfigs(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMutationLoggerRecordsSuccessfulWritesOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAuditDB(t)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", uint(7))
		c.Set("username", "alice")
	})
	r.Use(MutationLogger(db))
	r.PUT("/api/profile", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.POST("/api/explicit", func(c *gin.Context) {
		Log(db, c, "profile_update", "user", 7, "updated profile")
		c.Status(http.StatusNoContent)
	})

	for _, request := range []struct {
		method string
		path   string
	}{
		{http.MethodPut, "/api/profile"},
		{http.MethodPost, "/api/explicit"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(request.method, request.path, nil))
		if w.Code != http.StatusNoContent {
			t.Fatalf("%s %s: status %d", request.method, request.path, w.Code)
		}
	}

	var rows []database.AuditLog
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Action != "put_api_profile" || rows[1].Action != "profile_update" {
		t.Fatalf("unexpected audit rows: %#v", rows)
	}
}

func TestCleanupRespectsRetentionDays(t *testing.T) {
	db := setupAuditDB(t)
	if err := sysconfig.UpdateConfig(db, "log_retention_days", "7", 0); err != nil {
		t.Fatal(err)
	}
	old := database.AuditLog{Action: "old", CreatedAt: time.Now().AddDate(0, 0, -8)}
	recent := database.AuditLog{Action: "recent", CreatedAt: time.Now().AddDate(0, 0, -6)}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&recent).Error; err != nil {
		t.Fatal(err)
	}
	cleanup(db)
	var actions []string
	if err := db.Model(&database.AuditLog{}).Order("id").Pluck("action", &actions).Error; err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0] != "recent" {
		t.Fatalf("retention cleanup kept %v, want [recent]", actions)
	}
}

func TestMutationDescriptionsAndLegacyViews(t *testing.T) {
	for _, tc := range []struct{ method, path, want string }{
		{"POST", "/api/rental/tenants", "新增租户"},
		{"PUT", "/api/rental/tenants/:id", "修改租户"},
		{"POST", "/api/rental/meter-records", "保存抄表记录"},
		{"POST", "/api/rental/rooms/:id/apply-link", "生成或重置租客登记链接"},
		{"DELETE", "/api/rental/bills/:id", "删除账单"},
	} {
		label, _ := describeMutation(tc.method, tc.path)
		if label != tc.want {
			t.Fatalf("%s %s = %s", tc.method, tc.path, label)
		}
	}
	rows := toViews([]database.AuditLog{{Action: "put_api_rental_tenants_:id", Detail: "PUT /api/rental/tenants/:id"},
		{Action: "rental.tenant_account.bind", Detail: "关联租户登录账号：合成账号"}})
	if rows[0].Detail != "修改租户" || rows[0].ActionLabel != "修改租户" || rows[1].Detail != "关联租户登录账号：合成账号" {
		t.Fatalf("bad display mapping: %#v", rows)
	}
}
