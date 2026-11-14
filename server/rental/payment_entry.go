package rental

import (
	"errors"
	"strings"
	"time"

	"smallgo/server/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Optional preview version for mobile callers. Replays are checked before this
// version so the original request can still be recovered after the bill changes.
type paymentExpected struct {
	UpdatedAt  string  `json:"updated_at"`
	PaidAmount float64 `json:"paid_amount"`
	Payable    float64 `json:"payable"`
}

func (v *paymentExpected) matches(b *Bill) bool {
	if v == nil {
		return true
	}
	at, err := time.Parse(time.RFC3339Nano, v.UpdatedAt)
	return err == nil && at.Equal(b.UpdatedAt) && v.PaidAmount == b.PaidAmount && v.Payable == b.Payable
}

func handleBillPaymentStatus(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		key := c.Query("request_key")
		if len(key) < 8 || len(key) > 100 || strings.TrimSpace(key) != key {
			response.ErrorBadRequest(c, "请使用原收款请求编号核对")
			return
		}
		var bill *Bill
		var p Payment
		found, denied := false, false
		err := db.Transaction(func(tx *gorm.DB) error {
			var ok bool
			bill, ok = findUserBill(tx, c)
			if !ok {
				denied = true
				return errors.New("bill unavailable")
			}
			err := tx.Where("org_id = ? AND bill_id = ? AND request_key = ?", bill.LedgerID, bill.ID, key).First(&p).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			found = err == nil
			return err
		})
		if denied {
			return
		}
		if err != nil {
			response.ErrorInternal(c, "暂时无法核对实收，请稍后重试")
			return
		}
		var payment interface{}
		if found {
			payment = paymentView(p)
		}
		response.Success(c, gin.H{"found": found, "bill": bill, "payment": payment})
	}
}
