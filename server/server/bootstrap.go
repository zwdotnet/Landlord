package server

import (
	"crypto/subtle"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"os"
	"smallgo/server/database"
	"smallgo/server/response"
	"strings"
)

// Optional first-owner gate for a fresh public installation. The one-time
// invitation is sent in a header, never in a URL or response. After the first
// owner exists, ordinary registration follows the configured approval rules.
func bootstrapGate(db *gorm.DB) gin.HandlerFunc {
	key := os.Getenv("LANDLORD_SETUP_KEY")
	return func(c *gin.Context) {
		if key != "" && c.Request.Method == "POST" && strings.HasSuffix(c.Request.URL.Path, "/api/auth/register") {
			var count int64
			if err := db.Model(&database.User{}).Count(&count).Error; err != nil {
				response.ErrorInternal(c, "初始化状态读取失败")
				c.Abort()
				return
			}
			if count == 0 && subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Landlord-Setup")), []byte(key)) != 1 {
				response.ErrorForbidden(c, "请通过部署者提供的首次初始化链接创建管理员")
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
