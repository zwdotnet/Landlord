package user

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"smallgo/server/audit"
	"smallgo/server/database"
	"smallgo/server/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var contactPhone = regexp.MustCompile(`^1[3-9][0-9]{9}$`)
var errContactChanged = errors.New("contact changed")
var errContactDenied = errors.New("contact denied")

// Contact numbers are display/contact data, never an identity or association key.
// Changing them must not change username, WeChat identity, auth version or tenancy.
func handleContact(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		uid := c.GetUint("userID")
		var req struct {
			Phone    string  `json:"phone"`
			Expected *string `json:"expected_phone"`
		}
		if c.Request.Method == http.MethodPut {
			if c.ShouldBindJSON(&req) != nil || req.Expected == nil || !contactPhone.MatchString(strings.TrimSpace(req.Phone)) {
				response.ErrorBadRequest(c, "请填写正确的11位手机号并重新读取当前号码")
				return
			}
			req.Phone = strings.TrimSpace(req.Phone)
		}
		var u database.User
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.First(&u, uid).Error; err != nil {
				return err
			}
			if u.Role != RoleTenant || (u.Status != 1 && u.Status != 2) {
				return errContactDenied
			}
			if c.Request.Method != http.MethodPut || u.Phone == req.Phone {
				return nil
			}
			if u.Phone != *req.Expected {
				return errContactChanged
			}
			result := tx.Model(&database.User{}).Where("id = ? AND role = ? AND status IN ? AND phone = ?", uid, RoleTenant, []int{1, 2}, *req.Expected).Update("phone", req.Phone)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errContactChanged
			}
			u.Phone = req.Phone
			audit.Log(tx, c, "update", "user", uid, "租客修改本人联系手机号")
			return nil
		})
		switch {
		case errors.Is(err, errContactDenied):
			response.ErrorForbidden(c, "仅租客账号可修改联系手机号")
		case errors.Is(err, errContactChanged):
			response.Error(c, http.StatusConflict, response.CodeBadRequest, "手机号已在其他页面修改，请重新读取后再保存")
		case err != nil:
			response.ErrorInternal(c, "读取或保存手机号失败，请核对当前号码后重试")
		default:
			response.Success(c, gin.H{"phone": u.Phone, "display_name": u.Phone})
		}
	}
}
