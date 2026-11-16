package rental

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func orderedBatch(t *testing.T, env *testEnv, path string, contents ...[]byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for i, data := range contents {
		part, err := mw.CreateFormFile("files", fmt.Sprintf("batch-%d.pdf", i))
		if err != nil {
			t.Fatal(err)
		}
		part.Write(data)
	}
	mw.Close()
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+env.token)
	w := httptest.NewRecorder()
	env.r.ServeHTTP(w, req)
	return w
}

func TestUploadBatchFailureLeavesNoRecordsOrFiles(t *testing.T) {
	for _, kind := range []string{"attachments", "contracts"} {
		t.Run(kind, func(t *testing.T) {
			env := setupEnv(t)
			dir := t.TempDir()
			attachmentsDirOverride = dir
			contractsDirOverride = dir
			t.Cleanup(func() { attachmentsDirOverride = ""; contractsDirOverride = "" })
			room := env.seedRoom("batch", 1000, 0)
			tenant := env.seedTenant(room, "synthetic")
			path := fmt.Sprintf("/api/rental/tenants/%d/%s", tenant, kind)
			table := "tenant_attachments"
			if kind == "contracts" {
				table = "contracts"
			}
			count := func() int64 { var n int64; env.db.Table(table).Count(&n); return n }
			files := func() int {
				n := 0
				filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
					if err == nil && !e.IsDir() {
						n++
					}
					return nil
				})
				return n
			}
			for i := 0; i < 2; i++ {
				w := orderedBatch(t, env, path, pdfBytes(), []byte("invalid second file"))
				if w.Code != 400 {
					t.Fatalf("invalid status %d", w.Code)
				}
				if count() != 0 || files() != 0 {
					t.Fatal("failed batch partially persisted")
				}
			}
			trigger := fmt.Sprintf("CREATE TRIGGER reject_batch BEFORE INSERT ON %s WHEN NEW.file_name='batch-1.pdf' BEGIN SELECT RAISE(ABORT,'synthetic disk/database failure'); END", table)
			if err := env.db.Exec(trigger).Error; err != nil {
				t.Fatal(err)
			}
			w := orderedBatch(t, env, path, pdfBytes(), pdfBytes())
			if w.Code != 500 || count() != 0 || files() != 0 {
				t.Fatalf("DB failure retained batch: %d records=%d files=%d", w.Code, count(), files())
			}
			env.db.Exec("DROP TRIGGER reject_batch")
			// A storage path failure must also leave the database untouched.
			block := filepath.Join(dir, fmt.Sprintf("ledger-%d", env.personalLedger))
			os.RemoveAll(block)
			os.WriteFile(block, []byte("block directory"), 0600)
			w = orderedBatch(t, env, path, pdfBytes(), pdfBytes())
			if w.Code != 500 || count() != 0 {
				t.Fatal("disk failure retained records")
			}
			os.Remove(block)
			// Simulate a successful upload whose response is discarded. Readback
			// exposes both items; the client must reconcile rather than auto retry.
			_ = orderedBatch(t, env, path, pdfBytes(), pdfBytes())
			read := env.do(http.MethodGet, path, nil)
			if read.Code != 200 || count() != 2 || files() != 2 {
				t.Fatal("successful batch cannot be reconciled after response loss")
			}
		})
	}
}
