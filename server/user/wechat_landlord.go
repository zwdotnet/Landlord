package user

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"smallgo/server/audit"
	"smallgo/server/auth"
	"smallgo/server/database"
	"smallgo/server/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

func configuredWeChatClient() wechatClient {
	return wechatClient{
		appID: strings.TrimSpace(os.Getenv("WX_APP_ID")), secret: strings.TrimSpace(os.Getenv("WX_APP_SECRET")),
		http: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func landlordWeChatUser(u database.User) bool {
	return (u.Role == RoleUser || u.Role == RoleAdmin) && (u.Status == 1 || u.Status == 2)
}

// Separate from tenant login: no portal toggle, phone matching or account creation.
func landlordWeChatLoginHandler(db *gorm.DB, client wechatClient) gin.HandlerFunc {
	// SQL errors/slow-query logs must not interpolate OpenID or password hashes.
	db = db.Session(&gorm.Session{Logger: db.Logger.LogMode(gormlogger.Silent)})
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if client.appID == "" || client.secret == "" {
			response.Error(c, http.StatusServiceUnavailable, response.CodeForbidden, "微信登录暂未配置，请使用账号密码登录")
			return
		}
		var body struct {
			Code string `json:"code"`
		}
		if c.ShouldBindJSON(&body) != nil || strings.TrimSpace(body.Code) == "" || len(body.Code) > 512 {
			response.ErrorBadRequest(c, "请通过微信重新登录")
			return
		}
		openid, err := client.exchange(c.Request.Context(), body.Code)
		if err != nil {
			response.ErrorBadRequest(c, err.Error())
			return
		}
		// One SQL snapshot must contain both the binding and AuthVersion. Reading
		// the identity first and the user later could mint a post-unbind JWT with
		// the new version for an identity that has already been removed.
		var u database.User
		err = db.Where("id = (?)", db.Model(&WeChatIdentity{}).Select("user_id").Where("app_id = ? AND open_id = ?", client.appID, openid)).First(&u).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.ErrorForbidden(c, "此微信尚未绑定房东账号，请先用账号密码登录后绑定")
			return
		}
		if err != nil {
			response.ErrorInternal(c, "登录失败，请稍后重试")
			return
		}
		if !landlordWeChatUser(u) {
			response.ErrorForbidden(c, "此微信未关联可用的房东账号，请使用账号密码登录")
			return
		}
		secret := getJWTSecret(db)
		if secret == "" {
			response.ErrorInternal(c, "登录配置异常，请稍后重试")
			return
		}
		token, err := auth.GenerateTokenWithTTL(u.ID, u.Username, u.Role, u.AuthVersion, secret, loginTokenTTL(db))
		if err != nil {
			response.ErrorInternal(c, "登录失败，请重试")
			return
		}
		c.Set("userID", u.ID)
		c.Set("username", u.Username)
		audit.Log(db, c, "wechat_login", "user", u.ID, "房东通过已绑定微信登录")
		response.Success(c, gin.H{"token": token, "user": gin.H{"id": u.ID, "username": u.Username, "role": u.Role, "status": u.Status}})
	}
}

var errWeChatAccountChanged = errors.New("账号状态已改变，请重新登录后操作")
var errWeChatConflict = errors.New("微信或账号已存在其他绑定，请先解绑原关联；也可继续使用账号密码登录")

// Password is checked before exchanging a one-use code, then compared again in
// the transaction. The first statement obtains SQLite's write reservation and
// rejects concurrent password/status/version changes before touching a binding.
func lockWeChatAccount(tx *gorm.DB, u database.User) error {
	r := tx.Model(&database.User{}).Where("id = ? AND password = ? AND role = ? AND status = ? AND auth_version = ?", u.ID, u.Password, u.Role, u.Status, u.AuthVersion).
		UpdateColumn("auth_version", gorm.Expr("auth_version"))
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return errWeChatAccountChanged
	}
	return nil
}

// mode: status, bind or unbind. Only the authenticated account can be changed.
func landlordWeChatBindingHandler(db *gorm.DB, client wechatClient, mode string) gin.HandlerFunc {
	db = db.Session(&gorm.Session{Logger: db.Logger.LogMode(gormlogger.Silent)})
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		var u database.User
		if db.First(&u, c.GetUint("userID")).Error != nil || !landlordWeChatUser(u) {
			response.ErrorForbidden(c, "仅可管理本人房东账号的微信绑定")
			return
		}
		if mode == "status" {
			var identity WeChatIdentity
			err := db.Where("user_id = ?", u.ID).First(&identity).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				response.ErrorInternal(c, "读取绑定状态失败，请重试")
				return
			}
			response.Success(c, gin.H{"bound": err == nil, "current_app": err == nil && identity.AppID == client.appID, "available": client.appID != "" && client.secret != "", "status": u.Status})
			return
		}
		var body struct {
			Password string `json:"password"`
			Code     string `json:"code"`
		}
		if c.ShouldBindJSON(&body) != nil || body.Password == "" || len(body.Password) > 128 {
			response.ErrorBadRequest(c, "请输入当前账号密码")
			return
		}
		if verified, _ := verifyStoredPassword(u.Password, body.Password, ""); !verified {
			response.ErrorForbidden(c, "账号密码不正确，请重试")
			return
		}
		openid := ""
		if mode == "bind" {
			if u.Status != 1 {
				response.ErrorForbidden(c, "账号审核通过后才可绑定微信")
				return
			}
			if client.appID == "" || client.secret == "" {
				response.Error(c, http.StatusServiceUnavailable, response.CodeForbidden, "微信绑定暂未配置，请继续使用账号密码登录")
				return
			}
			if strings.TrimSpace(body.Code) == "" || len(body.Code) > 512 {
				response.ErrorBadRequest(c, "请重新取得微信登录凭证")
				return
			}
			var err error
			openid, err = client.exchange(c.Request.Context(), body.Code)
			if err != nil {
				response.ErrorBadRequest(c, err.Error())
				return
			}
		}
		changed := false
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := lockWeChatAccount(tx, u); err != nil {
				return err
			}
			if mode == "bind" {
				claim := WeChatIdentity{AppID: client.appID, OpenID: openid, UserID: u.ID}
				r := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&claim)
				if r.Error != nil {
					return r.Error
				}
				changed = r.RowsAffected == 1
				var current WeChatIdentity
				if err := tx.Where("user_id = ?", u.ID).First(&current).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return errWeChatConflict
					}
					return err
				}
				if current.AppID != client.appID || current.OpenID != openid {
					return errWeChatConflict
				}
				return nil
			}
			r := tx.Where("user_id = ?", u.ID).Delete(&WeChatIdentity{})
			if r.Error != nil {
				return r.Error
			}
			changed = r.RowsAffected > 0
			if changed {
				return tx.Model(&database.User{}).Where("id = ?", u.ID).UpdateColumn("auth_version", gorm.Expr("auth_version + 1")).Error
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, errWeChatConflict) || errors.Is(err, errWeChatAccountChanged) {
				response.Error(c, http.StatusConflict, http.StatusConflict, err.Error())
				return
			}
			response.ErrorInternal(c, "绑定操作未完成，请刷新状态后重试")
			return
		}
		if changed {
			if mode == "bind" {
				audit.Log(db, c, "wechat_bind", "user", u.ID, "房东绑定微信")
			} else {
				audit.Log(db, c, "wechat_unbind", "user", u.ID, "房东解绑微信并撤销全部应用登录令牌")
			}
		}
		response.Success(c, gin.H{"bound": mode == "bind", "requires_login": mode == "unbind" && changed})
	}
}
