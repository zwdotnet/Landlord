package rental

import (
	"context"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"io"
	"net/http"
	"net/url"
	"smallgo/server/apps"
	"smallgo/server/database"
	"smallgo/server/response"
	"strings"
	"time"
)

type PushIdentity struct {
	ID         uint `gorm:"primarykey"`
	PublicKey  string
	PrivateKey string `json:"-"`
	CreatedAt  time.Time
}
type BrowserSubscription struct {
	ID           uint      `gorm:"primarykey" json:"id"`
	UserID       uint      `gorm:"index" json:"-"`
	EndpointHash string    `gorm:"uniqueIndex" json:"-"`
	Endpoint     string    `json:"-"`
	P256dh       string    `json:"-"`
	Auth         string    `json:"-"`
	Active       bool      `json:"active"`
	LastStatus   string    `json:"last_status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
type PushDelivery struct {
	ID             uint      `gorm:"primarykey" json:"id"`
	NoticeID       uint      `gorm:"uniqueIndex:idx_push_delivery" json:"notice_id"`
	SubscriptionID uint      `gorm:"uniqueIndex:idx_push_delivery" json:"-"`
	UserID         uint      `gorm:"index" json:"-"`
	Status         string    `gorm:"index" json:"status"`
	Attempts       int       `json:"attempts"`
	LastStatus     string    `json:"last_status"`
	NextAttemptAt  time.Time `json:"next_attempt_at"`
	LeaseUntil     time.Time `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func init() {
	database.RegisterModels(&PushIdentity{}, &BrowserSubscription{}, &PushDelivery{})
	apps.Register(apps.App{Name: "landlord_webpush", DisplayName: "PWA 通知", SetupAuth: func(auth *gin.RouterGroup, db *gorm.DB) {
		g := auth.Group("/rental/push", approvalGate(db))
		g.GET("/config", func(c *gin.Context) {
			p, err := pushIdentity(db)
			if err != nil {
				response.ErrorInternal(c, "通知服务暂不可用")
				return
			}
			response.Success(c, gin.H{"public_key": p.PublicKey})
		})
		g.POST("/subscriptions", handlePushSubscribe(db))
		g.DELETE("/subscriptions", handlePushUnsubscribe(db))
		g.GET("/status", func(c *gin.Context) {
			var rows []PushDelivery
			db.Where("user_id = ?", currentUserID(c)).Order("id DESC").Limit(20).Find(&rows)
			response.Success(c, rows)
		})
		g.POST("/test", func(c *gin.Context) {
			var count int64
			db.Model(&BrowserSubscription{}).Where("user_id = ? AND active = ?", currentUserID(c), true).Count(&count)
			if count == 0 {
				response.ErrorBadRequest(c, "请先开启此设备通知")
				return
			}
			path := "/admin/notifications"
			var user database.User
			db.First(&user, currentUserID(c))
			if user.Role == "tenant" {
				path = "/portal/notifications"
			}
			n := InboxNotice{UserID: currentUserID(c), EventKey: "push-test:" + uuid.NewString(), Kind: "push_test", Title: "通知测试", Body: "这是一条由你主动发起的通知测试。", URL: path, CreatedAt: businessNow()}
			if err := db.Transaction(func(tx *gorm.DB) error { return createInbox(tx, &n) }); err != nil {
				response.ErrorInternal(c, "测试未创建")
				return
			}
			response.Success(c, gin.H{"queued": true})
			go dispatchWebPush(db, businessNow())
		})
	}})
	database.RegisterUserDeleteCleanup(func(db *gorm.DB, id uint) error {
		if err := db.Where("user_id = ?", id).Delete(&BrowserSubscription{}).Error; err != nil {
			return err
		}
		if err := db.Where("user_id = ?", id).Delete(&PushDelivery{}).Error; err != nil {
			return err
		}
		return db.Where("user_id = ?", id).Delete(&InboxNotice{}).Error
	})
}
func pushIdentity(db *gorm.DB) (PushIdentity, error) {
	var p PushIdentity
	if err := db.First(&p, 1).Error; err == nil {
		return p, nil
	}
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return p, err
	}
	p = PushIdentity{ID: 1, PublicKey: public, PrivateKey: private}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&p).Error; err != nil {
		return p, err
	}
	p = PushIdentity{}
	err = db.First(&p, 1).Error
	return p, err
}
func validPushSubscription(s *webpush.Subscription) bool {
	if len(s.Endpoint) > 4096 {
		return false
	}
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	allowed := false
	for _, h := range []string{"fcm.googleapis.com", "updates.push.services.mozilla.com", "push.apple.com", "notify.windows.com"} {
		if host == h || strings.HasSuffix(host, "."+h) {
			allowed = true
		}
	}
	if !allowed {
		return false
	}
	auth, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s.Keys.Auth, "="))
	if err != nil || len(auth) != 16 {
		return false
	}
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s.Keys.P256dh, "="))
	if err != nil {
		return false
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), key)
	return x != nil && y != nil
}
func handlePushSubscribe(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var sub webpush.Subscription
		if c.ShouldBindJSON(&sub) != nil || !validPushSubscription(&sub) {
			response.ErrorBadRequest(c, "浏览器推送订阅无效或当前推送服务不受支持")
			return
		}
		hash := sha256.Sum256([]byte(sub.Endpoint))
		row := BrowserSubscription{UserID: currentUserID(c), EndpointHash: hex.EncodeToString(hash[:]), Endpoint: sub.Endpoint, P256dh: sub.Keys.P256dh, Auth: sub.Keys.Auth, Active: true}
		err := db.Transaction(func(tx *gorm.DB) error {
			// A browser can change account. Possessing the subscription rebinds the
			// device, while old deliveries are cancelled by the audience check below.
			return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "endpoint_hash"}}, DoUpdates: clause.Assignments(map[string]interface{}{"user_id": row.UserID, "p256dh": row.P256dh, "auth": row.Auth, "active": true, "last_status": "", "updated_at": time.Now()})}).Create(&row).Error
		})
		if err != nil {
			response.ErrorInternal(c, "订阅未保存")
			return
		}
		response.Success(c, gin.H{"subscribed": true})
	}
}
func handlePushUnsubscribe(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Endpoint string `json:"endpoint"`
		}
		if c.ShouldBindJSON(&req) != nil {
			response.ErrorBadRequest(c, "参数错误")
			return
		}
		hash := sha256.Sum256([]byte(req.Endpoint))
		if err := db.Model(&BrowserSubscription{}).Where("user_id = ? AND endpoint_hash = ?", currentUserID(c), hex.EncodeToString(hash[:])).Update("active", false).Error; err != nil {
			response.ErrorInternal(c, "保存失败")
			return
		}
		response.Success(c, gin.H{"subscribed": false})
	}
}
func enqueueNoticePush(db *gorm.DB, n *InboxNotice) error {
	var subscriptions []BrowserSubscription
	if err := db.Where("user_id = ? AND active = ?", n.UserID, true).Find(&subscriptions).Error; err != nil {
		return err
	}
	for _, s := range subscriptions {
		row := PushDelivery{NoticeID: n.ID, SubscriptionID: s.ID, UserID: n.UserID, Status: "pending", NextAttemptAt: n.CreatedAt}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

// Only this boundary performs network I/O. Tests use the actual encryption
// library with a recording HTTP transport, without contacting real devices.
var sendBrowserPush = func(ctx context.Context, p *PushIdentity, s *BrowserSubscription, payload []byte) (int, error) {
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{Endpoint: s.Endpoint, Keys: webpush.Keys{P256dh: s.P256dh, Auth: s.Auth}}, &webpush.Options{Subscriber: "https://zoeng.site", VAPIDPublicKey: p.PublicKey, VAPIDPrivateKey: p.PrivateKey, TTL: 3600, HTTPClient: client})
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	return resp.StatusCode, nil
}

func dispatchWebPush(db *gorm.DB, now time.Time) {
	var rows []PushDelivery
	if db.Where("status = ? AND next_attempt_at <= ? AND (lease_until <= ? OR lease_until IS NULL)", "pending", now, now).Order("id").Limit(10).Find(&rows).Error != nil {
		return
	}
	for _, row := range rows {
		lease := now.Add(time.Minute)
		claimed := db.Model(&PushDelivery{}).Where("id = ? AND status = ? AND (lease_until <= ? OR lease_until IS NULL)", row.ID, "pending", now).Update("lease_until", lease)
		if claimed.Error != nil || claimed.RowsAffected != 1 {
			continue
		}
		var n InboxNotice
		var s BrowserSubscription
		if db.First(&n, row.NoticeID).Error != nil || db.First(&s, row.SubscriptionID).Error != nil || !s.Active || s.UserID != row.UserID || !noticeVisible(db, &n, row.UserID) {
			db.Model(&row).Updates(map[string]interface{}{"status": "cancelled", "last_status": "权限或订阅已变化"})
			continue
		}
		p, err := pushIdentity(db)
		status := 0
		if err == nil {
			// Privacy on the lock screen: business names, addresses and amounts remain
			// behind authenticated API access. The SW also checks the bound account.
			payload, _ := json.Marshal(map[string]interface{}{"title": "租务提醒", "body": "你有一条新提醒，打开平台查看。", "url": n.URL, "tag": fmt.Sprintf("landlord-notice-%d", n.ID), "user_id": n.UserID})
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			status, err = sendBrowserPush(ctx, &p, &s, payload)
			cancel()
		}
		attempts := row.Attempts + 1
		state := "failed"
		next := now
		label := fmt.Sprintf("推送服务 HTTP %d", status)
		if err == nil && status >= 200 && status < 300 {
			state = "accepted"
			label = "推送服务已接收（设备送达尚未确认）"
		} else if status == 404 || status == 410 {
			db.Model(&s).Updates(map[string]interface{}{"active": false, "last_status": "订阅已失效，请重新开启通知"})
		} else if (err != nil || status == 429 || status >= 500) && attempts < 6 {
			state = "pending"
			next = now.Add(time.Duration(1<<attempts) * time.Minute)
			label = "暂时无法投递，将自动重试"
		}
		db.Model(&row).Updates(map[string]interface{}{"status": state, "attempts": attempts, "last_status": label, "next_attempt_at": next, "lease_until": time.Time{}})
	}
}
