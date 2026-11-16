package rental

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"smallgo/server/config"
	"smallgo/server/database"
	ginserver "smallgo/server/server"
	"smallgo/server/sysconfig"
)

// The archive is exercised against real model records and protected HTTP file
// handlers, not just copied bytes in a mock directory.
func TestFullBackupRestoresUploadedFilesAndArchivedPDF(t *testing.T) {
	env := setupEnv(t)
	root := t.TempDir()
	source := filepath.Join(root, "source")
	os.MkdirAll(filepath.Join(source, "db"), 0700)
	oldAttachments, oldContracts := attachmentsDirOverride, contractsDirOverride
	attachmentsDirOverride = filepath.Join(source, "attachments")
	contractsDirOverride = filepath.Join(source, "contracts")
	t.Cleanup(func() { attachmentsDirOverride = oldAttachments; contractsDirOverride = oldContracts })
	room := env.seedRoom("backup", 1000, 0)
	tenant := env.seedTenant(room, "synthetic")
	for _, kind := range []string{"contracts", "attachments"} {
		data := pdfBytes()
		if kind == "attachments" {
			data = pngBytes(t)
		}
		w := orderedBatch(t, env, fmt.Sprintf("/api/rental/tenants/%d/%s", tenant, kind), data)
		if w.Code != 200 {
			t.Fatalf("seed %s: %d %s", kind, w.Code, w.Body.String())
		}
	}
	w := uploadMeterPhotos(t, env, env.token, fmt.Sprintf("/api/rental/rooms/%d/meter-photos", room), "2026-11", "water", map[string][]byte{"meter.png": pngBytes(t)})
	if w.Code != 200 {
		t.Fatalf("seed meter photo: %d %s", w.Code, w.Body.String())
	}
	// Store a synthetic immutable PDF using the actual archive model. Generation
	// and financial rollback paths remain covered by the existing PDF tests.
	doc := IssuedDocument{LedgerID: env.personalLedger, RequestKey: "backup-test", Number: "BACKUP-1", Kind: "receipt", PDF: pdfBytes(), FileSize: len(pdfBytes())}
	doc.SHA256 = fmt.Sprintf("%x", sha256.Sum256(doc.PDF))
	if err := env.db.Create(&doc).Error; err != nil {
		t.Fatal(err)
	}
	if err := env.db.Exec("VACUUM INTO ?", filepath.Join(source, "db", "rental.db")).Error; err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "backup.zip")
	destination := filepath.Join(root, "restored")
	pythonName := "python3"
	if runtime.GOOS == "windows" {
		pythonName = "python"
	}
	python, err := exec.LookPath(pythonName)
	if err != nil {
		python, err = exec.LookPath("python")
	}
	if err != nil {
		t.Fatal("Python is required for full-backup recovery test")
	}
	tool := filepath.Join("..", "..", "scripts", "full-backup.py")
	for _, args := range [][]string{{tool, "create", "--data-dir", source, "--output", archive, "--offline"}, {tool, "restore", archive, "--destination", destination}} {
		if output, err := exec.Command(python, args...).CombinedOutput(); err != nil {
			t.Fatalf("backup tool: %v %s", err, output)
		}
	}
	// Remove the source files to establish this really is blank-directory recovery.
	os.RemoveAll(filepath.Join(source, "attachments"))
	os.RemoveAll(filepath.Join(source, "contracts"))
	attachmentsDirOverride = filepath.Join(destination, "data", "attachments")
	contractsDirOverride = filepath.Join(destination, "data", "contracts")
	recovered, err := database.InitDB(filepath.Join(destination, "data", "db", "rental.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.CloseDB(recovered)
	secret, err := sysconfig.GetConfig(recovered, "jwt_secret", 0)
	if err != nil {
		t.Fatal(err)
	}
	env.r = ginserver.NewRouter(config.Config{CORSOrigin: "*", RateLimit: 0}, recovered, secret)
	var contracts []Contract
	var attachments []TenantAttachment
	var photos []MeterPhoto
	var docs []IssuedDocument
	recovered.Find(&contracts)
	recovered.Find(&attachments)
	recovered.Find(&photos)
	recovered.Find(&docs)
	if len(contracts) != 1 || len(attachments) != 1 || len(photos) != 1 || len(docs) != 1 || !strings.HasPrefix(string(docs[0].PDF), "%PDF-") {
		t.Fatal("restored records/PDF differ")
	}
	for _, path := range []string{fmt.Sprintf("/api/rental/contracts/%d/file", contracts[0].ID), fmt.Sprintf("/api/rental/attachments/%d/file", attachments[0].ID), fmt.Sprintf("/api/rental/meter-photos/%d/file", photos[0].ID), fmt.Sprintf("/api/rental/documents/%d/file?ledger_id=%d", docs[0].ID, env.personalLedger)} {
		// Same original IDs and authentication; only the restored file roots differ.
		w := env.do(http.MethodGet, path, nil)
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("restored authenticated download %s: %d", path, w.Code)
		}
		w = doJSON(env.r, http.MethodGet, path, "", nil)
		if w.Code != 401 {
			t.Fatalf("anonymous recovered file visible: %d", w.Code)
		}
	}
}
