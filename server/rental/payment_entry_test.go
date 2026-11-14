package rental

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func paymentEntryFixture(t *testing.T) (*testEnv, *Bill) {
	t.Helper()
	env, room, _ := documentFixture(t)
	bill := issueLandlordBill(t, env, room, "2030-01", nil)
	return env, &bill
}

func paymentEntryPath(b *Bill) string { return fmt.Sprintf("/api/rental/bills/%d/pay", b.ID) }
func paymentStatusPath(b *Bill, key string) string {
	return fmt.Sprintf("/api/rental/bills/%d/payment-status?request_key=%s", b.ID, key)
}
func expectedPayment(b *Bill) *paymentExpected {
	return &paymentExpected{b.UpdatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"), b.PaidAmount, b.Payable}
}

func TestPaymentEntryStatusAndVersionedReplay(t *testing.T) {
	env, bill := paymentEntryFixture(t)
	const key = "mini-payment-entry-once"
	w := env.do("GET", paymentStatusPath(bill, key), nil)
	if w.Code != 200 || field(t, decodeBody(t, w), "data", "found") != false || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing request was not safely queryable")
	}
	request := map[string]interface{}{"amount": 500, "note": "线下实收", "request_key": key, "expected": expectedPayment(bill)}
	w = env.do("POST", paymentEntryPath(bill), request)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for i := 0; i < 2; i++ {
		w = env.do("POST", paymentEntryPath(bill), request)
		if w.Code != 200 || num(field(t, decodeBody(t, w), "data", "paid_amount")) != 500 {
			t.Fatal("old expected version broke idempotent replay")
		}
	}
	w = env.do("GET", paymentStatusPath(bill, key), nil)
	if w.Code != 200 || field(t, decodeBody(t, w), "data", "found") != true || num(field(t, decodeBody(t, w), "data", "payment", "receipt_id")) == 0 {
		t.Fatal("receipt missing from recovered payment")
	}
	if strings.Contains(w.Body.String(), key) || strings.Contains(w.Body.String(), "request_digest") {
		t.Fatal("internal payment fields leaked")
	}
	request["amount"] = 600
	if env.do("POST", paymentEntryPath(bill), request).Code != 400 {
		t.Fatal("key accepted changed amount")
	}
	var n int64
	env.db.Model(&Payment{}).Count(&n)
	if n != 1 {
		t.Fatal("payment duplicated")
	}
}

func TestPaymentEntryStaleExpectedCreatesNoPaymentOrDocument(t *testing.T) {
	env, bill := paymentEntryFixture(t)
	before := expectedPayment(bill)
	env.db.Model(&Bill{}).Where("id = ?", bill.ID).Update("paid_amount", 100)
	w := env.do("POST", paymentEntryPath(bill), map[string]interface{}{"amount": 500, "request_key": "mini-stale-version", "expected": before})
	if w.Code != 409 {
		t.Fatal(w.Body.String())
	}
	var n int64
	env.db.Model(&Payment{}).Count(&n)
	if n != 0 {
		t.Fatal("stale payment saved")
	}
	env.db.Model(&IssuedDocument{}).Count(&n)
	if n != 0 {
		t.Fatal("stale receipt saved")
	}
	if env.do("POST", paymentEntryPath(bill), map[string]interface{}{"amount": 100, "request_key": "web-compatible-no-version"}).Code != 200 {
		t.Fatal("legacy Web payment broken")
	}
}

func TestPaymentEntryConcurrentSameKeyRecoversOneReceipt(t *testing.T) {
	env, bill := paymentEntryFixture(t)
	const key = "mini-same-key-concurrent"
	request := map[string]interface{}{"amount": 500, "request_key": key, "expected": expectedPayment(bill)}
	start := make(chan struct{})
	done := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() { <-start; done <- env.do("POST", paymentEntryPath(bill), request).Code }()
	}
	close(start)
	for i := 0; i < 2; i++ {
		if code := <-done; code != 200 && code != 400 && code != 409 {
			t.Fatalf("unexpected concurrent status %d", code)
		}
	}
	w := env.do("GET", paymentStatusPath(bill, key), nil)
	if w.Code != 200 || field(t, decodeBody(t, w), "data", "found") != true {
		t.Fatal("concurrent payment cannot be recovered")
	}
	var payments, receipts int64
	env.db.Model(&Payment{}).Count(&payments)
	env.db.Model(&IssuedDocument{}).Count(&receipts)
	if payments != 1 || receipts != 1 {
		t.Fatal("duplicate payment or receipt")
	}
}

func TestPaymentEntryFailureAndPermissionChangeRollBack(t *testing.T) {
	for _, kind := range []string{"renderer", "permission", "document-insert"} {
		t.Run(kind, func(t *testing.T) {
			env, bill := paymentEntryFixture(t)
			if kind == "renderer" {
				documentPDFRenderer = func(context.Context, string) ([]byte, error) { return nil, errors.New("injected renderer failure") }
			}
			if kind == "permission" {
				documentPDFRenderer = func(_ context.Context, html string) ([]byte, error) {
					env.db.Model(&LedgerMember{}).Where("org_id = ? AND user_id = ?", bill.LedgerID, env.adminID).Update("status", "removed")
					return []byte("%PDF-1.4\n" + html), nil
				}
			}
			if kind == "document-insert" {
				env.db.Exec(`CREATE TRIGGER fail_receipt BEFORE INSERT ON issued_documents BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`)
			}
			w := env.do("POST", paymentEntryPath(bill), map[string]interface{}{"amount": 500, "request_key": "mini-rollback-" + kind})
			if w.Code == 200 {
				t.Fatal("failed transaction was accepted")
			}
			var count int64
			env.db.Model(&Payment{}).Count(&count)
			if count != 0 {
				t.Fatal("partial payment persisted")
			}
			env.db.Model(&IssuedDocument{}).Count(&count)
			if count != 0 {
				t.Fatal("partial receipt persisted")
			}
		})
	}
}

func TestPaymentEntryStatusIsScopedToBillAndLedger(t *testing.T) {
	env, bill := paymentEntryFixture(t)
	shared := createSharedLedger(t, env, env.token, "实收只读测试")
	env.db.Model(&Bill{}).Where("id = ?", bill.ID).Update("org_id", shared)
	env.db.Model(&Room{}).Where("id = ?", bill.RoomID).Update("org_id", shared)
	env.db.Model(&BillItem{}).Where("bill_id = ?", bill.ID).Update("org_id", shared)
	bill.LedgerID = shared
	const key = "mini-scoped-payment"
	if env.do("POST", paymentEntryPath(bill)+"?ledger_id="+itoa(bill.LedgerID), map[string]interface{}{"amount": 500, "request_key": key}).Code != 200 {
		t.Fatal("fixture payment")
	}
	other := newPersonalLedger(t, env, "mini-payment-other")
	if w := doJSON(env.r, "GET", paymentStatusPath(bill, key)+"&ledger_id="+itoa(other.ledgerID), other.token, nil); w.Code != 404 {
		t.Fatal("cross ledger status readable")
	}
	reader := newPersonalLedger(t, env, "mini-payment-reader")
	inviteAndAccept(t, env, env.token, reader.token, bill.LedgerID, "mini-payment-reader")
	if err := env.db.Model(&LedgerMember{}).Where("org_id = ? AND user_id = ?", bill.LedgerID, reader.userID).Update("perms", `{"bills":"view"}`).Error; err != nil {
		t.Fatal(err)
	}
	w := doJSON(env.r, "GET", paymentStatusPath(bill, key)+"&ledger_id="+itoa(bill.LedgerID), reader.token, nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := doJSON(env.r, "POST", paymentEntryPath(bill)+"?ledger_id="+itoa(bill.LedgerID), reader.token, map[string]interface{}{"amount": 100, "request_key": "readonly-pay"}); w.Code != 403 {
		t.Fatal("readonly member wrote payment")
	}
}
