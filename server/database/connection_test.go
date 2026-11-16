package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteConnectionPolicyAndLegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec("CREATE TABLE parent(id INTEGER PRIMARY KEY); CREATE TABLE child(id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id)); INSERT INTO parent VALUES(1); INSERT INTO child VALUES(1,1)"); err != nil {
		t.Fatal(err)
	}
	legacy.Close()
	for i := 0; i < 2; i++ {
		db, err := InitDB(path)
		if err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{"journal_mode": "wal", "foreign_keys": "1", "synchronous": "1", "busy_timeout": "5000"} {
			var got string
			if err := db.Raw("PRAGMA " + key).Scan(&got).Error; err != nil || got != want {
				t.Fatalf("%s got=%s want=%s err=%v", key, got, want, err)
			}
		}
		if err := db.Exec("INSERT INTO child VALUES(2,999)").Error; err == nil {
			t.Fatal("orphan accepted")
		}
		pool, _ := db.DB()
		pool.SetMaxIdleConns(0) // force the next statement onto a fresh pooled connection
		if err := db.Exec("INSERT INTO child VALUES(3,999)").Error; err == nil {
			t.Fatal("fresh connection lost foreign-key policy")
		}
		var n int64
		db.Raw("SELECT COUNT(*) FROM child").Scan(&n)
		if n != 1 {
			t.Fatal("legacy rows changed")
		}
		CloseDB(db)
	}
}

func TestSQLiteRejectsLegacyForeignKeyViolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.db")
	db, _ := sql.Open("sqlite3", path)
	_, err := db.Exec("CREATE TABLE parent(id INTEGER PRIMARY KEY); CREATE TABLE child(id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id)); INSERT INTO child VALUES(1,999)")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	opened, err := InitDB(path)
	if opened != nil {
		CloseDB(opened)
	}
	if err == nil || !strings.Contains(err.Error(), "foreign_key_check") {
		t.Fatalf("expected diagnostic, got %v", err)
	}
}
