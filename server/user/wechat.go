package user

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"smallgo/server/audit"
	"smallgo/server/auth"
	"smallgo/server/database"
	"smallgo/server/response"
	"smallgo/server/sysconfig"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AppID scopes OpenID. Neither OpenID nor session_key is a client credential.
type WeChatIdentity struct {
	ID        uint   `gorm:"primarykey"`
	AppID     string `gorm:"uniqueIndex:idx_wechat_identity;not null"`
	OpenID    string `gorm:"uniqueIndex:idx_wechat_identity;not null"`
	UserID    uint   `gorm:"uniqueIndex;not null"`
	CreatedAt time.Time
}

func init() {
	database.RegisterModels(&WeChatIdentity{})
	database.RegisterUserDeleteCleanup(func(db *gorm.DB, userID uint) error {
		return db.Where("user_id = ?", userID).Delete(&WeChatIdentity{}).Error
	})
}

type wechatClient struct {
	appID, secret string
	http          *http.Client
}

func (w wechatClient) exchange(ctx context.Context, code string) (string, error) {
	query := url.Values{"appid": {w.appID}, "secret": {w.secret}, "js_code": {code}, "grant_type": {"authorization_code"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.weixin.qq.com/sns/jscode2session?"+query.Encode(), nil)
	if err != nil {
		return "", errors.New("微信登录请求失败")
	}
	res, err := w.http.Do(req)
	// Do not return a transport error: it may contain the secret-bearing URL.
	if err != nil {
		return "", errors.New("暂时无法连接微信，请稍后重试")
	}
	defer res.Body.Close()
	var data struct {
		OpenID  string `json:"openid"`
		ErrCode int    `json:"errcode"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&data) != nil {
		return "", errors.New("微信登录响应异常，请重试")
	}
	if data.ErrCode != 0 || data.OpenID == "" || len(data.OpenID) > 128 {
		return "", errors.New("微信登录凭证已失效，请重新登录")
	}
	return data.OpenID, nil
}

var wechatPhonePattern = regexp.MustCompile(`^1[3-9][0-9]{9}$`)

func handleWeChatLogin(db *gorm.DB) gin.HandlerFunc {
	return wechatLoginHandler(db, wechatClient{
		appID: strings.TrimSpace(os.Getenv("WX_APP_ID")), secret: strings.TrimSpace(os.Getenv("WX_APP_SECRET")),
		http: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }},
	})
}

func wechatLoginHandler(db *gorm.DB, client wechatClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if client.appID == "" || client.secret == "" {
			response.Error(c, http.StatusServiceUnavailable, response.CodeForbidden, "微信登录暂未配置，请联系房东")
			return
		}
		var count int64
		if err := db.Model(&database.User{}).Count(&count).Error; err != nil || count == 0 {
			response.Error(c, http.StatusForbidden, response.CodeForbidden, "平台尚未初始化，请联系房东")
			return
		}
		portal, err := sysconfig.GetConfig(db, tenantPortalConfigKey, 0)
		if err != nil || portal != "true" {
			response.Error(c, http.StatusForbidden, response.CodeForbidden, "本应用未开放租客登录")
			return
		}
		var body struct {
			Code  string `json:"code"`
			Phone string `json:"phone"`
		}
		if c.ShouldBindJSON(&body) != nil || strings.TrimSpace(body.Code) == "" || len(body.Code) > 512 {
			response.ErrorBadRequest(c, "请通过微信重新登录")
			return
		}
		body.Phone = strings.TrimSpace(body.Phone)
		if body.Phone != "" && !wechatPhonePattern.MatchString(body.Phone) {
			response.ErrorBadRequest(c, "请填写正确的入住手机号")
			return
		}
		openid, err := client.exchange(c.Request.Context(), body.Code)
		if err != nil {
			response.ErrorBadRequest(c, err.Error())
			return
		}
		var identity WeChatIdentity
		err = db.Where("app_id = ? AND open_id = ?", client.appID, openid).First(&identity).Error
		if errors.Is(err, gorm.ErrRecordNotFound) && body.Phone == "" {
			response.Success(c, gin.H{"phone_required": true})
			return
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			response.ErrorInternal(c, "登录失败，请稍后重试")
			return
		}
		var u database.User
		err = db.Transaction(func(tx *gorm.DB) error {
			// Claim the identity before creating an account. A unique AppID/OpenID
			// constraint plus the transaction prevents concurrent first logins
			// from leaving duplicate or orphan accounts.
			claim := WeChatIdentity{AppID: client.appID, OpenID: openid}
			result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "app_id"}, {Name: "open_id"}}, DoNothing: true}).Create(&claim)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				if body.Phone == "" {
					return errors.New("请填写入住手机号")
				}
				password, hashErr := auth.HashPassword(uuid.NewString() + uuid.NewString())
				if hashErr != nil {
					return hashErr
				}
				u = database.User{Username: "wx_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12], Password: password,
					Role: RoleTenant, Status: 2, AuthVersion: 1, Phone: body.Phone}
				if err := tx.Create(&u).Error; err != nil {
					return err
				}
				if err := database.RunUserCreateHooks(tx, u.ID); err != nil {
					return err
				}
				if err := tx.Model(&claim).Update("user_id", u.ID).Error; err != nil {
					return err
				}
			} else {
				if err := tx.Where("app_id = ? AND open_id = ?", client.appID, openid).First(&identity).Error; err != nil {
					return err
				}
				if err := tx.First(&u, identity.UserID).Error; err != nil {
					return err
				}
			}
			if u.Role != RoleTenant || (u.Status != 1 && u.Status != 2) {
				return errors.New("账号不可用，请联系房东")
			}
			// A manually typed phone is contact information only. Never match or
			// take over an existing account/tenancy based on this field.
			return nil
		})
		if err != nil {
			response.Error(c, http.StatusForbidden, response.CodeForbidden, "微信登录失败或账号不可用，请联系房东后重试")
			return
		}
		secret := getJWTSecret(db)
		if secret == "" {
			response.ErrorInternal(c, "登录配置异常，请联系房东")
			return
		}
		token, err := auth.GenerateTokenWithTTL(u.ID, u.Username, u.Role, u.AuthVersion, secret, loginTokenTTL(db))
		if err != nil {
			response.ErrorInternal(c, "登录失败，请重试")
			return
		}
		c.Set("userID", u.ID)
		c.Set("username", u.Username)
		audit.Log(db, c, "wechat_login", "user", u.ID, "租客通过微信登录")
		response.Success(c, gin.H{"token": token, "user": gin.H{"id": u.ID, "username": u.Username, "role": u.Role, "status": u.Status}})
	}
}
