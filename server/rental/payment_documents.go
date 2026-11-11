package rental

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"smallgo/server/response"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func paymentDigest(amount float64, note string, items []payItemReq) string {
	raw, _ := json.Marshal(struct {
		Amount float64
		Note   string
		Items  []payItemReq
	}{round2(amount), strings.TrimSpace(note), items})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func paymentView(p Payment) gin.H {
	return gin.H{"id": p.ID, "bill_id": p.BillID, "paid_at": p.PaidAt, "amount": p.Amount,
		"note": p.Note, "items": paymentItems(p.Items), "receipt_id": p.ReceiptID, "reversal_of_id": p.ReversalOfID, "voided_at": p.VoidedAt, "void_reason": p.VoidReason}
}

func newPaymentDocument(ctx context.Context, db *gorm.DB, bill *Bill, payment *Payment, reversed *Payment) (*IssuedDocument, error) {
	name, contact, _ := receiptHeaderFor(db, bill.LedgerID)
	if name == "" || contact == "" {
		var room Room
		if err := db.Where("id = ? AND org_id = ?", bill.RoomID, bill.LedgerID).First(&room).Error; err != nil {
			return nil, err
		}
		if name == "" {
			name = room.LandlordName
		}
		if contact == "" {
			contact = room.LandlordPhone
		}
	}
	kind, title := "receipt", "收款收据"
	lines := []documentLine{{"房屋 / 租客", bill.RoomNo + " / " + bill.TenantName}, {"对应账单", bill.ReceiptNo + "（" + bill.Period + "）"},
		{"本次线下实收", formatAmount(payment.Amount) + " 元"}, {"登记时间", payment.PaidAt.Format("2006-01-02 15:04:05 -07:00")},
		{"收款方", name + " " + contact}, {"备注", payment.Note}}
	if reversed != nil {
		kind, title = "reversal", "收款冲销凭证"
		lines[2] = documentLine{"本次冲销金额", formatAmount(-payment.Amount) + " 元"}
		lines = append(lines, documentLine{"原收款记录", fmt.Sprintf("%d", reversed.ID)}, documentLine{"冲销原因", payment.Note})
		if reversed.ReceiptID > 0 {
			var original IssuedDocument
			if documentQuery(db).First(&original, reversed.ReceiptID).Error == nil {
				lines = append(lines, documentLine{"原收据编号", original.Number})
			}
		}
	}
	for _, share := range paymentItems(payment.Items) {
		lines = append(lines, documentLine{share.Name, formatAmount(share.Amount) + " 元"})
	}
	footer := "本凭证记录本次实际收款，不代表整张账单已结清。重新下载读取本次归档原件。"
	if reversed != nil {
		footer = "本凭证用于更正登记错误，原收款与原收据仍保留；冲销不表示已向租客实际退款。"
	}
	doc, err := buildIssuedDocument(ctx, kind, payment.RequestKey, bill.LedgerID, payment.UserID, documentSnapshot{Title: title, Lines: lines, Footer: footer})
	if err != nil {
		return nil, err
	}
	doc.BillID = bill.ID
	return doc, nil
}

// Commit the aggregate, immutable receipt and payment in one transaction.
// The expected amount prevents stale concurrent requests from losing money.
func commitDocumentedPayment(db *gorm.DB, bill *Bill, payment *Payment, doc *IssuedDocument, expected float64, original *Payment) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if original != nil {
			now := time.Now()
			updated := tx.Model(&Payment{}).Where("id = ? AND org_id = ? AND voided_at IS NULL", original.ID, original.LedgerID).
				Updates(map[string]interface{}{"voided_at": now, "void_reason": payment.Note})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return errors.New("该收款已被冲销，请刷新")
			}
		}
		if err := tx.Create(payment).Error; err != nil {
			return err
		}
		doc.PaymentID = payment.ID
		if err := tx.Create(doc).Error; err != nil {
			return err
		}
		payment.ReceiptID = doc.ID
		if err := tx.Model(payment).UpdateColumn("receipt_id", doc.ID).Error; err != nil {
			return err
		}
		bill.PaidAmount = round2(expected + payment.Amount)
		bill.PaidAt = &payment.PaidAt
		if payment.Amount < 0 {
			bill.PaidAt = nil
			var latest Payment
			if tx.Where("bill_id = ? AND org_id = ? AND amount > 0 AND voided_at IS NULL", bill.ID, bill.LedgerID).Order("paid_at DESC, id DESC").First(&latest).Error == nil {
				bill.PaidAt = &latest.PaidAt
			}
		}
		if bill.PaidAmount <= feeTolerance {
			bill.PaidAt = nil
		}
		bill.recalc()
		updated := tx.Model(&Bill{}).Where("id = ? AND org_id = ? AND paid_amount = ? AND payable = ? AND total_amount = ? AND updated_at = ?", bill.ID, bill.LedgerID, expected, bill.Payable, bill.TotalAmount, bill.UpdatedAt).
			Updates(map[string]interface{}{"paid_amount": bill.PaidAmount, "paid_at": bill.PaidAt, "status": bill.Status})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errors.New("账单收款已变化，请使用原请求编号重试")
		}
		var room Room
		if err := tx.Where("id = ? AND org_id = ?", bill.RoomID, bill.LedgerID).First(&room).Error; err != nil {
			return err
		}
		if _, err := recalcCarryoverFrom(tx, &room, bill.Period); err != nil {
			return err
		}
		bill.LastPaymentID, bill.LastReceiptID = payment.ID, doc.ID
		return nil
	})
}

func handlePaymentReverse(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var original Payment
		if db.Where("id = ?", c.Param("id")).First(&original).Error != nil || !inLedgerScope(c, original.LedgerID) {
			response.ErrorNotFound(c, "收款不存在")
			return
		}
		if !requireLedgerWrite(db, c, original.LedgerID, ModuleBills, PermEdit) {
			return
		}
		var req struct {
			Reason     string `json:"reason"`
			RequestKey string `json:"request_key"`
		}
		if c.ShouldBindJSON(&req) != nil || len([]rune(strings.TrimSpace(req.Reason))) < 1 || len([]rune(req.Reason)) > 500 || len(req.RequestKey) > 100 {
			response.ErrorBadRequest(c, "请填写 1 至 500 字的冲销原因")
			return
		}
		if req.RequestKey == "" {
			req.RequestKey = uuid.NewString()
		}
		var bill Bill
		if db.Where("id = ? AND org_id = ?", original.BillID, original.LedgerID).First(&bill).Error != nil {
			response.ErrorNotFound(c, "账单不存在")
			return
		}
		var replay Payment
		if db.Where("org_id = ? AND request_key = ?", original.LedgerID, req.RequestKey).First(&replay).Error == nil {
			if replay.ReversalOfID == nil || *replay.ReversalOfID != original.ID || replay.Note != strings.TrimSpace(req.Reason) {
				response.ErrorBadRequest(c, "请求编号已用于其他收款或冲销")
				return
			}
			bill.LastPaymentID, bill.LastReceiptID = replay.ID, replay.ReceiptID
			response.Success(c, bill)
			return
		}
		if original.Amount <= 0 || original.ReversalOfID != nil {
			response.ErrorBadRequest(c, "冲销记录不能再次冲销")
			return
		}
		if original.VoidedAt != nil {
			response.ErrorBadRequest(c, "这笔收款已冲销，请刷新查看原凭证和冲销记录")
			return
		}
		if bill.PaidAmount+feeTolerance < original.Amount {
			response.ErrorBadRequest(c, "账单累计实收与原流水不符，请先核对历史数据")
			return
		}
		shares := paymentItems(original.Items)
		for i := range shares {
			shares[i].Amount = -shares[i].Amount
		}
		payment := Payment{UserID: currentUserID(c), LedgerID: original.LedgerID, BillID: bill.ID, PaidAt: time.Now(), Amount: -original.Amount,
			Items: paymentItemsJSON(shares), Note: strings.TrimSpace(req.Reason), RequestKey: req.RequestKey, ReversalOfID: &original.ID}
		doc, err := newPaymentDocument(c.Request.Context(), db, &bill, &payment, &original)
		if err != nil {
			response.ErrorInternal(c, err.Error()+"；尚未登记冲销")
			return
		}
		if err := commitDocumentedPayment(db, &bill, &payment, doc, bill.PaidAmount, &original); err != nil {
			response.ErrorBadRequest(c, "冲销未保存，请刷新核对后重试")
			return
		}
		response.Success(c, bill)
	}
}
