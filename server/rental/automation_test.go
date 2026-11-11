package rental

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	webpush "github.com/SherClockHolmes/webpush-go"
	"golang.org/x/crypto/hkdf"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func saveTestPlan(t *testing.T, env *testEnv, room, tid uint, extra map[string]interface{}) AutomationPlan {
	t.Helper()
	req := map[string]interface{}{"room_id": room, "tenant_ids": []uint{tid}, "enabled": true, "start_period": "2030-01", "meter_day": 7, "bill_day": 7, "due_day": 10, "lead_days": 3}
	for k, v := range extra {
		req[k] = v
	}
	w := env.do(http.MethodPost, "/api/rental/automation/plans", req)
	if w.Code != 200 {
		t.Fatalf("plan %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Data AutomationPlan `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	return result.Data
}
func taskFor(t *testing.T, env *testEnv, plan uint, period string) CycleTask {
	t.Helper()
	var task CycleTask
	if err := env.db.Where("plan_id = ? AND period = ?", plan, period).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	return task
}
func autoRun(t *testing.T, env *testEnv, date string) {
	t.Helper()
	now, err := time.ParseInLocation("2006-01-02 15:04", date, businessZone)
	if err != nil {
		t.Fatal(err)
	}
	if err := runAutomation(env.db, now, nil); err != nil {
		t.Fatal(err)
	}
}
func TestM3WaitingMeterResumeIdempotencyAndTenantNotice(t *testing.T) {
	env, room, tid := documentFixture(t)
	uid, token := registerTenant(t, env, "autonotice")
	bindTenantAccount(t, env, env.token, tid, uid)
	plan := saveTestPlan(t, env, room, tid, nil)
	autoRun(t, env, "2030-01-04 09:00")
	autoRun(t, env, "2030-01-04 09:01")
	var count int64
	env.db.Model(&InboxNotice{}).Where("kind = ?", "meter").Count(&count)
	if count != 1 {
		t.Fatalf("reminder duplicate %d", count)
	}
	autoRun(t, env, "2030-01-07 09:00")
	if taskFor(t, env, plan.ID, "2030-01").Status != "waiting_meter" {
		t.Fatal("missing reading should wait")
	}
	env.db.Model(&Bill{}).Count(&count)
	if count != 0 {
		t.Fatal("missing readings issued a zero-use bill")
	}
	w := env.do(http.MethodPost, "/api/rental/meter-records", map[string]interface{}{"room_id": room, "period": "2030-01", "water": 12, "elec": 100, "gas": 0})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	autoRun(t, env, "2030-01-07 09:02")
	autoRun(t, env, "2030-01-07 09:03")
	task := taskFor(t, env, plan.ID, "2030-01")
	if task.Status != "issued" || task.BillID == 0 || task.DueDate != "2030-01-10" {
		t.Fatalf("task %#v", task)
	}
	var bill Bill
	env.db.First(&bill, task.BillID)
	if bill.Rent != 2000 || bill.WaterFee != 60 || bill.ElecFee != 120 || bill.AutomationTaskID != task.ID {
		t.Fatalf("bill %#v", bill)
	}
	env.db.Model(&Bill{}).Count(&count)
	if count != 1 {
		t.Fatal("repeat run issued another bill")
	}
	w = portalGet(env, token, "/api/rental/notifications")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "新账单待核对") {
		t.Fatal(w.Body.String())
	}
	var notice InboxNotice
	env.db.Where("user_id = ? AND kind = ?", uid, "bill").First(&notice)
	if w := doJSON(env.r, http.MethodPost, fmt.Sprintf("/api/rental/notifications/%d/read", notice.ID), token, nil); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var read InboxNotice
	env.db.First(&read, notice.ID)
	if read.ReadAt == nil {
		t.Fatal("read state not persisted")
	}
	_, other := registerLandlord(t, env, "autonoaccess")
	if w := doJSON(env.r, http.MethodPost, fmt.Sprintf("/api/rental/notifications/%d/read", notice.ID), other, nil); w.Code != 404 {
		t.Fatal("foreign notice readable")
	}
	if w := doJSON(env.r, http.MethodPost, "/api/rental/automation/plans", token, map[string]interface{}{"room_id": room}); w.Code != 403 {
		t.Fatal("tenant can automate landlord charges")
	}
}
func TestM3CatchupOrderProrationQuarterlyAndContractEnd(t *testing.T) {
	env, room, tid := documentFixture(t)
	env.db.Model(&Tenant{}).Where("id = ?", tid).Updates(map[string]interface{}{"pay_cycle": "quarterly", "deposit": 1000, "lease_end_date": "2030-03-15"})
	plan := saveTestPlan(t, env, room, tid, nil)
	env.db.Create(&MeterRecord{LedgerID: env.personalLedger, RoomID: room, Period: "2030-02", Water: 20, Elec: 200})
	autoRun(t, env, "2030-03-10 09:00")
	if taskFor(t, env, plan.ID, "2030-02").Status != "waiting_previous" {
		t.Fatal("later month bypassed missing predecessor")
	}
	for i, period := range []string{"2030-01", "2030-03"} {
		env.db.Create(&MeterRecord{LedgerID: env.personalLedger, RoomID: room, Period: period, Water: float64(10 + 20*i), Elec: float64(100 + 200*i)})
	}
	autoRun(t, env, "2030-03-10 09:02")
	var bills []Bill
	env.db.Order("period").Find(&bills)
	if len(bills) != 3 || bills[0].DepositFee != 1000 || bills[1].DepositFee != 0 || bills[1].Rent != 0 || bills[2].Rent != 0 {
		t.Fatalf("quarterly/catchup %#v", bills)
	}
	want := round2(4000 + 2000.0*15/31)
	if bills[0].Rent != want {
		t.Fatalf("partial final month rent %v want %v", bills[0].Rent, want)
	}
	autoRun(t, env, "2030-04-10 09:00")
	if taskFor(t, env, plan.ID, "2030-04").Status != "cancelled" {
		t.Fatal("charged past contract end")
	}
}
func TestM3TermsChangeManualBillAndFailureRetry(t *testing.T) {
	env, room, tid := documentFixture(t)
	p := saveTestPlan(t, env, room, tid, nil)
	env.db.Model(&Room{}).Where("id = ?", room).Update("default_rent", 2100)
	autoRun(t, env, "2030-01-07 09:00")
	if taskFor(t, env, p.ID, "2030-01").Status != "review" {
		t.Fatal("new rate silently charged")
	}
	saveTestPlan(t, env, room, tid, nil)
	env.db.Create(&MeterRecord{LedgerID: env.personalLedger, RoomID: room, Period: "2030-01", Water: 10, Elec: 100})
	env.db.Exec("CREATE TRIGGER fail_cycle_bill BEFORE INSERT ON bills BEGIN SELECT RAISE(ABORT,'injected write failure'); END")
	autoRun(t, env, "2030-01-07 09:02")
	task := taskFor(t, env, p.ID, "2030-01")
	if task.Status != "retry" || task.BillID != 0 {
		t.Fatalf("retry state %#v", task)
	}
	var n int64
	env.db.Model(&Bill{}).Count(&n)
	if n != 0 {
		t.Fatal("failed cycle left a partial bill")
	}
	env.db.Exec("DROP TRIGGER fail_cycle_bill")
	autoRun(t, env, "2030-01-07 09:08")
	task = taskFor(t, env, p.ID, "2030-01")
	if task.Status != "issued" {
		t.Fatal("restart retry did not resume")
	}
	bill := issueLandlordBill(t, env, room, "2030-02", nil)
	autoRun(t, env, "2030-02-07 09:00")
	if taskFor(t, env, p.ID, "2030-02").BillID != bill.ID {
		t.Fatal("manual bill was replaced")
	}
}
func TestM3ReminderRespectsRevokedMembershipAndPlanAuthority(t *testing.T) {
	env, room, tid := documentFixture(t)
	plan := saveTestPlan(t, env, room, tid, nil)
	autoRun(t, env, "2030-01-04 09:00")
	env.db.Model(&LedgerMember{}).Where("user_id = ? AND org_id = ?", env.adminID, env.personalLedger).Update("status", MemberStatusRejected)
	w := env.do(http.MethodGet, "/api/rental/notifications", nil)
	if strings.Contains(w.Body.String(), "抄表待办") {
		t.Fatal("revoked member reads prior notice")
	}
	autoRun(t, env, "2030-01-07 09:00")
	if taskFor(t, env, plan.ID, "2030-01").Status != "review" {
		t.Fatal("revoked author can still issue bills")
	}
}
func deviceSubscription(t *testing.T) (webpush.Subscription, []byte) {
	t.Helper()
	priv, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	return webpush.Subscription{Endpoint: "https://web.push.apple.com/test-device", Keys: webpush.Keys{P256dh: base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y)), Auth: base64.RawURLEncoding.EncodeToString(auth)}}, priv
}
func TestM4PushQueueRetryRevocationAndExpiration(t *testing.T) {
	env := setupEnv(t)
	sub, _ := deviceSubscription(t)
	w := env.do(http.MethodPost, "/api/rental/push/subscriptions", sub)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	private := sendBrowserPush
	t.Cleanup(func() { sendBrowserPush = private })
	sent := 0
	sendBrowserPush = func(_ context.Context, _ *PushIdentity, _ *BrowserSubscription, p []byte) (int, error) {
		sent++
		if bytes.Contains(p, []byte("账单金额")) {
			t.Fatal("private data on lock screen")
		}
		return 503, nil
	}
	now := businessNow()
	n := InboxNotice{UserID: env.adminID, EventKey: "test-once", Kind: "push_test", Title: "账单金额", Body: "private", CreatedAt: now}
	if err := createInbox(env.db, &n); err != nil {
		t.Fatal(err)
	}
	again := InboxNotice{UserID: env.adminID, EventKey: "test-once", Kind: "push_test", CreatedAt: now}
	createInbox(env.db, &again)
	var count int64
	env.db.Model(&PushDelivery{}).Count(&count)
	if count != 1 {
		t.Fatal("duplicate push delivery")
	}
	dispatchWebPush(env.db, now.Add(time.Second))
	dispatchWebPush(env.db, now.Add(time.Second))
	if sent != 1 {
		t.Fatal("backoff ignored")
	}
	sendBrowserPush = func(context.Context, *PushIdentity, *BrowserSubscription, []byte) (int, error) {
		sent++
		return 410, nil
	}
	dispatchWebPush(env.db, now.Add(3*time.Minute))
	var device BrowserSubscription
	env.db.First(&device)
	if device.Active {
		t.Fatal("expired endpoint left active")
	}
	// Restrict endpoint hosts; no caller may use push delivery to probe LAN hosts.
	sub.Endpoint = "https://127.0.0.1/private"
	if w := env.do(http.MethodPost, "/api/rental/push/subscriptions", sub); w.Code != 400 {
		t.Fatal("SSRF endpoint accepted")
	}
	sub.Endpoint = "https://web.push.apple.com.evil.example/x"
	if validPushSubscription(&sub) {
		t.Fatal("suffix bypass")
	}
}

type pushCapture struct {
	t       *testing.T
	device  webpush.Subscription
	private []byte
	payload string
}

func (p *pushCapture) Do(req *http.Request) (*http.Response, error) {
	p.t.Helper()
	raw, _ := io.ReadAll(req.Body)
	if req.Header.Get("Content-Encoding") != "aes128gcm" || !strings.HasPrefix(req.Header.Get("Authorization"), "vapid ") {
		p.t.Fatal("missing web push protocol headers")
	}
	salt := raw[:16]
	size := int(raw[20])
	serverKey := raw[21 : 21+size]
	encrypted := raw[21+size:]
	x, y := elliptic.Unmarshal(elliptic.P256(), serverKey)
	sx, _ := elliptic.P256().ScalarMult(x, y, p.private)
	secret := sx.FillBytes(make([]byte, 32))
	auth, _ := base64.RawURLEncoding.DecodeString(p.device.Keys.Auth)
	clientKey, _ := base64.RawURLEncoding.DecodeString(p.device.Keys.P256dh)
	info := append([]byte("WebPush: info\x00"), clientKey...)
	info = append(info, serverKey...)
	ikm := make([]byte, 32)
	io.ReadFull(hkdf.New(sha256.New, secret, auth, info), ikm)
	key := make([]byte, 16)
	io.ReadFull(hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: aes128gcm\x00")), key)
	nonce := make([]byte, 12)
	io.ReadFull(hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: nonce\x00")), nonce)
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	plain, err := aead.Open(nil, nonce, encrypted, nil)
	if err != nil {
		p.t.Fatal(err)
	}
	plain = bytes.TrimRight(plain, "\x00")
	if len(plain) == 0 || plain[len(plain)-1] != 2 {
		p.t.Fatal("invalid record delimiter")
	}
	plain = plain[:len(plain)-1]
	if string(plain) != p.payload {
		p.t.Fatalf("encrypted payload mismatch %q", plain)
	}
	return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
}
func TestM4ActualWebPushEncryptionCanBeDecryptedByDeviceKey(t *testing.T) {
	env := setupEnv(t)
	identity, err := pushIdentity(env.db)
	if err != nil {
		t.Fatal(err)
	}
	device, priv := deviceSubscription(t)
	payload := `{"title":"租务提醒","user_id":1}`
	// The real library performs VAPID signing and RFC8291 encryption. A device-key
	// decryptor checks the emitted wire request, without claiming OS delivery.
	resp, err := webpush.SendNotificationWithContext(context.Background(), []byte(payload), &device, &webpush.Options{VAPIDPublicKey: identity.PublicKey, VAPIDPrivateKey: identity.PrivateKey, Subscriber: "https://zoeng.site", TTL: 3600, HTTPClient: &pushCapture{t, device, priv, payload}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatal(resp.StatusCode)
	}
	public := (env.do(http.MethodGet, "/api/rental/push/config", nil)).Body.String()
	if strings.Contains(public, identity.PrivateKey) {
		t.Fatal("private VAPID key disclosed")
	}
}
func TestM3OverlappingRoomPlansCannotDoubleCharge(t *testing.T) {
	env, room, tid := documentFixture(t)
	p := saveTestPlan(t, env, room, tid, nil)
	second := env.seedTenantWithLease(room, "共同承租人", "2030-12-31")
	env.db.Model(&Tenant{}).Where("id = ?", second).Update("move_in_date", "2030-01-01")
	req := map[string]interface{}{"room_id": room, "tenant_ids": []uint{second}, "enabled": true, "start_period": "2030-01"}
	if w := env.do(http.MethodPost, "/api/rental/automation/plans", req); w.Code != 400 {
		t.Fatalf("overlap allowed: %d %s", w.Code, w.Body.String())
	}
	env.db.Model(&AutomationPlan{}).Where("id = ?", p.ID).Update("enabled", false)
	// A bill already containing the first tenant must still stop a newly grouped
	// joint plan from charging that same tenant twice in the same period.
	issueLandlordBill(t, env, room, "2030-01", map[string]interface{}{"tenant_ids": []uint{tid}})
	joint := saveTestPlan(t, env, room, tid, map[string]interface{}{"tenant_ids": []uint{tid, second}})
	autoRun(t, env, "2030-01-07 09:00")
	if taskFor(t, env, joint.ID, "2030-01").Status != "review" {
		t.Fatal("joint plan duplicated an existing tenant's bill")
	}
}
