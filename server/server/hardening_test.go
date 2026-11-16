package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"smallgo/server/config"
	"smallgo/server/database"
	"smallgo/server/middleware"
	"smallgo/server/version"
)

func TestSnapshotBuildIdentityAndFailure(t *testing.T) {
	dir := t.TempDir()
	db, err := database.InitDB(filepath.Join(dir, "rental.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.CloseDB(db)
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	v, b, c := version.Version, version.BuildTime, version.GitCommit
	t.Cleanup(func() { version.Version = v; version.BuildTime = b; version.GitCommit = c })
	version.Version = "0.10.0"
	version.BuildTime = "2026-11-15T00:00:00Z"
	version.GitCommit = "commit-one"
	first, _, err := preUpgradeSnapshot(db, dir, version.Version)
	if err != nil || first == "" {
		t.Fatalf("snapshot: %s %v", first, err)
	}
	again, _, err := preUpgradeSnapshot(db, dir, version.Version)
	if err != nil || again != "" {
		t.Fatal("same build repeated snapshot")
	}
	version.GitCommit = "commit-two"
	second, _, err := preUpgradeSnapshot(db, dir, version.Version)
	if err != nil || second == "" || second == first {
		t.Fatalf("changed commit skipped: %v", err)
	}
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "backups"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := preUpgradeSnapshot(db, bad, version.Version); err == nil {
		t.Fatal("snapshot failure ignored")
	}
	if _, err := os.Stat(filepath.Join(bad, snapshotMarkerFile)); !os.IsNotExist(err) {
		t.Fatal("failed snapshot marked complete")
	}
	version.GitCommit = "unknown"
	version.BuildTime = "unknown"
	if fp := binaryFingerprint(); fp == "0.10.0|unknown|unknown" {
		t.Fatal("unstamped executable has no identity")
	}
}

func TestExplicitTrustedProxyLimits(t *testing.T) {
	_, db, _ := setupTestRouter(t)
	for _, trusted := range []bool{false, true} {
		cfg := config.Config{}
		if trusted {
			cfg.TrustedProxies = []string{"10.20.0.2/32"}
		}
		r := NewRouter(cfg, db, "test-secret")
		rl := middleware.NewRateLimiter(1, time.Minute)
		r.GET("/source", middleware.LimitRequests(rl), func(c *gin.Context) { c.String(200, c.ClientIP()) })
		for i, ip := range []string{"198.51.100.1", "198.51.100.1", "198.51.100.2"} {
			req := httptest.NewRequest("GET", "/source", nil)
			req.RemoteAddr = "10.20.0.2:1234"
			req.Header.Set("X-Forwarded-For", ip)
			req.Header.Set("X-Real-IP", ip)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			want := http.StatusTooManyRequests
			if i == 0 || (trusted && i == 2) {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("trusted=%v request=%d status=%d", trusted, i, w.Code)
			}
		}
		// A client outside the configured hop cannot supply a trusted header.
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/source", nil)
			req.RemoteAddr = "203.0.113.5:10"
			req.Header.Set("X-Forwarded-For", string(rune('a'+i)))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if i == 1 && w.Code != 429 {
				t.Fatal("untrusted source bypassed limit")
			}
		}
	}
}

func TestSnapshotFailureStopsBeforeMigration(t *testing.T) {
	if dir := os.Getenv("LANDLORD_SNAPSHOT_FAILURE_CHILD"); dir != "" {
		config.C = config.Config{DataDir: dir, DBPath: filepath.Join(dir, "db", "rental.db"), LogDir: filepath.Join(dir, "logs"), LogMode: "release"}
		Start()
		t.Fatal("startup returned without failing")
	}
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "db"), 0700)
	db, err := database.InitDB(filepath.Join(dir, "db", "rental.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE users(id INTEGER PRIMARY KEY); INSERT INTO users VALUES(1)").Error; err != nil {
		t.Fatal(err)
	}
	database.CloseDB(db)
	os.WriteFile(filepath.Join(dir, "backups"), []byte("block snapshot"), 0600)
	cmd := exec.Command(os.Args[0], "-test.run=^TestSnapshotFailureStopsBeforeMigration$")
	cmd.Env = append(os.Environ(), "LANDLORD_SNAPSHOT_FAILURE_CHILD="+dir)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("startup must fail: %s", output)
	}
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("unexpected exit: %v %s", err, output)
	}
	db, err = database.InitDB(filepath.Join(dir, "db", "rental.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.CloseDB(db)
	var n int64
	db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE name='system_configs'").Scan(&n)
	if n != 0 {
		t.Fatal("migration ran despite failed safety snapshot")
	}
}
