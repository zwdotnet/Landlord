package rental

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func documentFixture(t *testing.T) (*testEnv, uint, uint) {
	env := setupEnv(t)
	// This deterministic renderer exercises snapshot persistence and escaping;
	// actual Chromium PDF layout is verified against a running app separately.
	documentPDFRenderer = func(_ context.Context, html string) ([]byte, error) { return []byte("%PDF-1.4\n" + html), nil }
	room := env.seedRoom("DOC-101", 2000, 0)
	env.db.Model(&Room{}).Where("id = ?", room).Updates(map[string]interface{}{"landlord_name": "测试房东", "landlord_phone": "{{PHONE_gj00gbxa}}", "address": "测试路一号"})
	tid := env.seedTenantWithLease(room, "测试租客", "2030-12-31")
	env.db.Model(&Tenant{}).Where("id = ?", tid).Update("move_in_date", "2030-01-01")
	return env, room, tid
}

func issueTestAgreement(t *testing.T, env *testEnv, tid uint, key string, extra map[string]interface{}) uint {
	t.Helper()
	req := map[string]interface{}{"kind": "lease", "request_key": key}
	for k, v := range extra {
		req[k] = v
	}
	w := env.do(http.MethodPost, fmt.Sprintf("/api/rental/tenants/%d/agreements", tid), req)
	if w.Code != http.StatusOK {
		t.Fatalf("issue agreement: %d %s", w.Code, w.Body.String())
	}
	return uint(num(field(t, decodeBody(t, w), "data", "id")))
}

func TestM2AgreementReprintIsImmutableAndTemplatesAreVersioned(t *testing.T) {
	env, room, tid := documentFixture(t)
	id := issueTestAgreement(t, env, tid, "agreement-original-001", nil)
	before := env.do(http.MethodGet, fmt.Sprintf("/api/rental/documents/%d/file", id), nil)
	if before.Code != http.StatusOK || !strings.Contains(before.Body.String(), "测试租客") {
		t.Fatal(before.Body.String())
	}
	env.db.Model(&Tenant{}).Where("id = ?", tid).Update("name", "更名后的租客")
	env.db.Model(&Room{}).Where("id = ?", room).Update("default_rent", 9000)
	w := env.do(http.MethodPost, "/api/rental/platform/document-templates", map[string]interface{}{"kind": "lease", "title": "新模板", "clauses": []string{"新的正文条款"}})
	if w.Code != http.StatusOK || num(field(t, decodeBody(t, w), "data", "version")) != 2 {
		t.Fatal(w.Body.String())
	}
	after := env.do(http.MethodGet, fmt.Sprintf("/api/rental/documents/%d/file", id), nil)
	if !bytes.Equal(before.Body.Bytes(), after.Body.Bytes()) {
		t.Fatal("reprinting changed the original file")
	}
	if retry := issueTestAgreement(t, env, tid, "agreement-original-001", nil); retry != id {
		t.Fatal("retry created a second contract")
	}
	newID := issueTestAgreement(t, env, tid, "agreement-newversion-002", nil)
	var doc IssuedDocument
	env.db.First(&doc, newID)
	if doc.TemplateVersion != 2 || !strings.Contains(doc.Snapshot, "更名后的租客") {
		t.Fatal("new version did not use new data")
	}
	if env.db.Model(&IssuedDocument{}).Where("id = ?", id).Update("snapshot", "rewrite").Error == nil {
		t.Fatal("issued snapshot can be overwritten")
	}
	if env.db.Delete(&doc).Error == nil {
		t.Fatal("issued document can be deleted")
	}
	if w := env.do(http.MethodDelete, fmt.Sprintf("/api/rental/tenants/%d", tid), nil); w.Code != http.StatusBadRequest {
		t.Fatal("tenant with archived contract could be deleted")
	}
}

func TestM2RenewalPDFUsesSelectedHistoricalTerm(t *testing.T) {
	env, _, tid := documentFixture(t)
	var renewalIDs []uint
	for _, rent := range []float64{2500, 3000} {
		w := env.do(http.MethodPost, fmt.Sprintf("/api/rental/tenants/%d/renew", tid), map[string]interface{}{"months": 12, "rent": rent})
		if w.Code != http.StatusOK {
			t.Fatal(w.Body.String())
		}
		renewalIDs = append(renewalIDs, uint(num(field(t, decodeBody(t, w), "data", "renewal", "id"))))
	}
	id := issueTestAgreement(t, env, tid, "renewal-document-first", map[string]interface{}{"kind": "renewal", "renewal_id": renewalIDs[0]})
	var doc IssuedDocument
	env.db.First(&doc, id)
	if !strings.Contains(doc.Snapshot, "2031-01-01 至 2031-12-31") || !strings.Contains(doc.Snapshot, "2500.00") || strings.Contains(doc.Snapshot, "3000.00") {
		t.Fatal("renewal PDF used the latest mutable tenant dates or rent")
	}
	initial := issueTestAgreement(t, env, tid, "initial-document-after-renewal", nil)
	var first IssuedDocument
	env.db.First(&first, initial)
	if !strings.Contains(first.Snapshot, "2030-01-01 至 2030-12-31") || !strings.Contains(first.Snapshot, "2000.00") {
		t.Fatal("initial term extended by future renewal")
	}
}

func TestM2DocumentScopeAndIntegrity(t *testing.T) {
	env, _, tid := documentFixture(t)
	id := issueTestAgreement(t, env, tid, "scope-agreement-key", nil)
	uid, token := registerTenant(t, env, "documentrenter")
	bindTenantAccount(t, env, env.token, tid, uid)
	path := fmt.Sprintf("/api/rental/portal/documents/%d/file", id)
	if w := portalGet(env, token, path); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	otherUID, otherToken := registerTenant(t, env, "otherdocumentrenter")
	otherRoom := env.seedRoom("DOC-202", 1000, 0)
	otherTenant := env.seedTenant(otherRoom, "测试租客")
	bindTenantAccount(t, env, env.token, otherTenant, otherUID)
	if w := portalGet(env, otherToken, path); w.Code != http.StatusNotFound {
		t.Fatal("another renter read the document")
	}
	if w := doJSON(env.r, http.MethodGet, fmt.Sprintf("/api/rental/documents/%d/file", id), "", nil); w.Code != http.StatusUnauthorized {
		t.Fatal("anonymous access allowed")
	}
	_, landlordToken := registerLandlord(t, env, "otherdocumentowner")
	if w := doJSON(env.r, http.MethodGet, fmt.Sprintf("/api/rental/documents/%d/file", id), landlordToken, nil); w.Code != http.StatusNotFound {
		t.Fatal("another landlord read the document")
	}
	if w := doJSON(env.r, http.MethodPost, "/api/rental/platform/document-templates", landlordToken, map[string]interface{}{"kind": "lease", "title": "非法版本", "clauses": []string{"正文"}}); w.Code != http.StatusForbidden {
		t.Fatal("landlord published platform template")
	}
	env.db.Exec("UPDATE issued_documents SET pdf = ? WHERE id = ?", []byte("tampered"), id)
	if w := portalGet(env, token, path); w.Code != http.StatusInternalServerError {
		t.Fatal("corrupted PDF served")
	}
}

func TestM2ArchivedDocumentsProtectRoomHistory(t *testing.T) {
	env, roomID, tid := documentFixture(t)
	issueTestAgreement(t, env, tid, "preserve-room-history", nil)
	env.db.Model(&Tenant{}).Where("id = ?", tid).Update("active", false)
	if w := env.do(http.MethodDelete, fmt.Sprintf("/api/rental/rooms/%d", roomID), nil); w.Code != http.StatusBadRequest {
		t.Fatalf("room deletion removed contract parent: %d %s", w.Code, w.Body.String())
	}
	var room Room
	if err := env.db.First(&room, roomID).Error; err != nil {
		t.Fatal(err)
	}
	originalLedger := room.LedgerID
	if _, err := moveRoomToLedger(env.db, &room, 9999); !errors.Is(err, errRoomHasIssuedDocuments) {
		t.Fatalf("room move must preserve archive scope: %v", err)
	}
	var tenant Tenant
	env.db.First(&tenant, tid)
	env.db.First(&room, roomID)
	if tenant.LedgerID != originalLedger || room.LedgerID != originalLedger {
		t.Fatal("rejected move changed archive parent scope")
	}
}

func TestM2AgreementCapturesExcludedFeesAndDefaultPrices(t *testing.T) {
	env, _, tid := documentFixture(t)
	var tenant Tenant
	env.db.First(&tenant, tid)
	tenant.ExcludedFees = []string{feeKeyGas}
	if err := env.db.Save(&tenant).Error; err != nil {
		t.Fatal(err)
	}
	id := issueTestAgreement(t, env, tid, "fee-agreement-snapshot", nil)
	var doc IssuedDocument
	env.db.First(&doc, id)
	if !strings.Contains(doc.Snapshot, "不向乙方收取") || !strings.Contains(doc.Snapshot, "1.20 元/度") || !strings.Contains(doc.Snapshot, "元/吨") {
		t.Fatalf("fee agreement missing exclusions, default price or units: %s", doc.Snapshot)
	}
}

func TestM2PaymentReceiptIdempotencyReversalAndRollback(t *testing.T) {
	env, room, _ := documentFixture(t)
	enableCarryover(t, env)
	bill := issueLandlordBill(t, env, room, "2030-01", nil)
	next := issueLandlordBill(t, env, room, "2030-02", nil)
	path := fmt.Sprintf("/api/rental/bills/%d/pay", bill.ID)
	req := map[string]interface{}{"amount": 500, "request_key": "payment-once-key", "note": "线下收款"}
	w := env.do(http.MethodPost, path, req)
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	pid := uint(num(field(t, decodeBody(t, w), "data", "last_payment_id")))
	docID := uint(num(field(t, decodeBody(t, w), "data", "last_receipt_id")))
	original := env.do(http.MethodGet, fmt.Sprintf("/api/rental/documents/%d/file", docID), nil).Body.Bytes()
	w = env.do(http.MethodPost, path, req)
	if w.Code != http.StatusOK || num(field(t, decodeBody(t, w), "data", "paid_amount")) != 500 {
		t.Fatal("duplicate request counted twice")
	}
	req["amount"] = 600
	if w := env.do(http.MethodPost, path, req); w.Code != http.StatusBadRequest {
		t.Fatal("same key accepted changed payload")
	}
	if w := env.do(http.MethodPut, fmt.Sprintf("/api/rental/bills/%d", bill.ID), map[string]interface{}{"room_id": room, "period": "2030-01", "paid_amount": 0}); w.Code != http.StatusBadRequest {
		t.Fatal("manual edit bypassed receipt ledger")
	}
	csv := strings.Join(legacyBillColumns, ",") + "\nDOC-101,测试租客,2030-01,2000,0,0,0,0,0,0,5,1.2,3.5,0,0,0,覆盖实收\n"
	if w := env.importCSVBody(csv); num(field(t, decodeBody(t, w), "data", "failed")) != 1 {
		t.Fatal("CSV overwrote a bill with archived receipts")
	}
	w = env.do(http.MethodPost, fmt.Sprintf("/api/rental/payments/%d/reverse", pid), map[string]interface{}{"reason": "金额登记错误", "request_key": "reverse-once-key"})
	if w.Code != http.StatusOK || num(field(t, decodeBody(t, w), "data", "paid_amount")) != 0 {
		t.Fatal(w.Body.String())
	}
	if retry := env.do(http.MethodPost, fmt.Sprintf("/api/rental/payments/%d/reverse", pid), map[string]interface{}{"reason": "金额登记错误", "request_key": "reverse-once-key"}); retry.Code != http.StatusOK {
		t.Fatalf("same reversal request should replay safely: %s", retry.Body.String())
	}
	if w := env.do(http.MethodPost, fmt.Sprintf("/api/rental/payments/%d/reverse", pid), map[string]interface{}{"reason": "重复冲销"}); w.Code != http.StatusBadRequest {
		t.Fatal("double reversal allowed")
	}
	after := env.do(http.MethodGet, fmt.Sprintf("/api/rental/documents/%d/file", docID), nil)
	if !bytes.Equal(original, after.Body.Bytes()) {
		t.Fatal("original receipt changed during reversal")
	}
	var later Bill
	env.db.First(&later, next.ID)
	if later.CarryIn != bill.Payable {
		t.Fatalf("reversal carry=%v want=%v", later.CarryIn, bill.Payable)
	}
	var sum float64
	env.db.Model(&Payment{}).Where("bill_id = ?", bill.ID).Select("SUM(amount)").Scan(&sum)
	if sum != 0 {
		t.Fatal("reversal changed original instead of adding balancing entry")
	}
	if w := env.do(http.MethodDelete, fmt.Sprintf("/api/rental/bills/%d", bill.ID), nil); w.Code != http.StatusBadRequest {
		t.Fatal("receipt parent bill deleted")
	}
	documentPDFRenderer = func(context.Context, string) ([]byte, error) { return nil, errors.New("renderer unavailable") }
	if w := env.do(http.MethodPost, path, map[string]interface{}{"amount": 100, "request_key": "failed-render-key"}); w.Code != http.StatusInternalServerError {
		t.Fatal("failed renderer was accepted")
	}
	var count int64
	env.db.Model(&Payment{}).Where("request_key = ?", "failed-render-key").Count(&count)
	if count != 0 {
		t.Fatal("payment persisted without receipt")
	}
}

func TestM2BillEditedWhileRenderingDoesNotCommitStalePayment(t *testing.T) {
	env, room, _ := documentFixture(t)
	bill := issueLandlordBill(t, env, room, "2030-01", nil)
	documentPDFRenderer = func(_ context.Context, html string) ([]byte, error) {
		if err := env.db.Model(&Bill{}).Where("id = ?", bill.ID).Updates(map[string]interface{}{"rent": 2500, "total_amount": 2500, "payable": 2500}).Error; err != nil {
			t.Fatal(err)
		}
		return []byte("%PDF-1.4\n" + html), nil
	}
	w := env.do(http.MethodPost, fmt.Sprintf("/api/rental/bills/%d/pay", bill.ID), map[string]interface{}{"amount": 500, "request_key": "concurrent-bill-change"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("stale bill payment: %d %s", w.Code, w.Body.String())
	}
	var payments, documents int64
	env.db.Model(&Payment{}).Where("bill_id = ?", bill.ID).Count(&payments)
	env.db.Model(&IssuedDocument{}).Where("bill_id = ?", bill.ID).Count(&documents)
	var current Bill
	env.db.First(&current, bill.ID)
	if payments != 0 || documents != 0 || current.PaidAmount != 0 || current.Payable != 2500 {
		t.Fatal("stale payment was not fully rolled back")
	}
}
