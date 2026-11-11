package rental

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"smallgo/server/apps"
	"smallgo/server/database"
	"smallgo/server/response"
	"time"
)

type InboxNotice struct {
	TaskID    uint       `json:"task_id"`
	ID        uint       `gorm:"primarykey" json:"id"`
	UserID    uint       `gorm:"index;uniqueIndex:idx_notice_event" json:"-"`
	EventKey  string     `gorm:"uniqueIndex:idx_notice_event" json:"-"`
	LedgerID  uint       `gorm:"column:org_id;index" json:"-"`
	Module    string     `json:"-"`
	TenantID  uint       `json:"-"`
	BillID    uint       `json:"bill_id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	URL       string     `json:"url"`
	ReadAt    *time.Time `json:"read_at"`
	CreatedAt time.Time  `json:"created_at"`
}

func init() {
	database.RegisterModels(&InboxNotice{})
	apps.Register(apps.App{Name: "landlord_inbox", DisplayName: "站内消息", SetupAuth: func(auth *gin.RouterGroup, db *gorm.DB) {
		g := auth.Group("/rental", approvalGate(db))
		g.GET("/notifications", handleInboxList(db))
		g.POST("/notifications/:id/read", handleInboxRead(db))
	}})
}
func noticeVisible(db *gorm.DB, n *InboxNotice, userID uint) bool {
	if n.UserID != userID {
		return false
	}
	var user database.User
	if db.First(&user, userID).Error != nil || user.Status != 1 {
		return false
	}
	if n.Kind == "push_test" {
		return true
	}
	if n.TenantID > 0 {
		if user.Role != "tenant" {
			return false
		}
		t, ok := portalTenant(db, userID)
		if !ok || t.ID != n.TenantID || t.LedgerID != n.LedgerID {
			return false
		}
		var bill Bill
		if db.First(&bill, n.BillID).Error != nil {
			return false
		}
		for _, id := range bill.RecipientTenantIDs {
			if id == t.ID {
				return true
			}
		}
		return false
	}
	if user.Role == "tenant" {
		return false
	}
	if n.Kind == "meter" && n.TaskID > 0 {
		var task CycleTask
		var plan AutomationPlan
		if db.First(&task, n.TaskID).Error != nil || db.First(&plan, task.PlanID).Error != nil || !plan.Enabled || task.Status == "issued" || task.Status == "cancelled" {
			return false
		}
		var readings int64
		if db.Model(&MeterRecord{}).Where("room_id = ? AND org_id = ? AND period = ? AND note <> ?", task.RoomID, n.LedgerID, task.Period, "随账单生成").Count(&readings).Error != nil || readings > 0 {
			return false
		}
	}
	return permRank[ledgerPerms(db, userID, n.LedgerID)[n.Module]] >= permRank[PermView]
}
func createInbox(tx *gorm.DB, n *InboxNotice) error {
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(n).Error; err != nil {
		return err
	}
	if n.ID == 0 {
		if err := tx.Where("user_id = ? AND event_key = ?", n.UserID, n.EventKey).First(n).Error; err != nil {
			return err
		}
	}
	return enqueueNoticePush(tx, n)
}
func notifyMeterTask(db *gorm.DB, p *AutomationPlan, t *CycleTask, now time.Time) error {
	// No duplicate reminders after reading is already available.
	var reading int64
	if err := db.Model(&MeterRecord{}).Where("org_id = ? AND room_id = ? AND period = ? AND note <> ?", p.LedgerID, p.RoomID, t.Period, "随账单生成").Count(&reading).Error; err != nil {
		return err
	}
	if reading > 0 {
		return nil
	}
	var members []LedgerMember
	if err := db.Where("org_id = ? AND status = ?", p.LedgerID, MemberStatusActive).Find(&members).Error; err != nil {
		return err
	}
	for _, m := range members {
		if permRank[ledgerPerms(db, m.UserID, p.LedgerID)[ModuleMeters]] < permRank[PermView] {
			continue
		}
		n := InboxNotice{UserID: m.UserID, EventKey: fmt.Sprintf("meter:%d:%s", p.ID, t.Period), LedgerID: p.LedgerID, Module: ModuleMeters, Kind: "meter", TaskID: t.ID, Title: "抄表待办", Body: fmt.Sprintf("%s · %s，计划 %s 抄表，%s 出账。缺读数将等待补录。", p.RoomNo, t.Period, t.MeterDate, t.BillDate), URL: "/admin/automation", CreatedAt: now}
		if err := createInbox(db, &n); err != nil {
			return err
		}
	}
	return nil
}
func notifyBillTenants(db *gorm.DB, b *Bill, now time.Time) error {
	if len(b.RecipientTenantIDs) == 0 {
		return nil
	}
	var tenants []Tenant
	if err := db.Where("id IN ? AND org_id = ? AND account_user_id > 0", b.RecipientTenantIDs, b.LedgerID).Find(&tenants).Error; err != nil {
		return err
	}
	for _, t := range tenants {
		due := "请按合同约定缴费"
		if b.DueDate != "" {
			due = "缴费期限 " + b.DueDate
		}
		n := InboxNotice{UserID: t.AccountUserID, EventKey: fmt.Sprintf("bill:%d:tenant:%d", b.ID, t.ID), LedgerID: b.LedgerID, TenantID: t.ID, BillID: b.ID, Kind: "bill", Title: "新账单待核对", Body: fmt.Sprintf("%s · %s，应缴 %.2f 元；%s。", b.RoomNo, b.Period, b.Payable, due), URL: "/portal/bills", CreatedAt: now}
		if err := createInbox(db, &n); err != nil {
			return err
		}
	}
	return nil
}
func handleInboxList(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Revalidate current ledger membership / tenant association on every read.
		// A former member cannot retain data access through a historical message.
		var candidates []InboxNotice
		if db.Where("user_id = ?", currentUserID(c)).Order("id DESC").Limit(1000).Find(&candidates).Error != nil {
			response.ErrorInternal(c, "读取消息失败")
			return
		}
		rows := make([]InboxNotice, 0)
		unread := 0
		for _, n := range candidates {
			if noticeVisible(db, &n, currentUserID(c)) {
				if n.ReadAt == nil {
					unread++
				}
				if len(rows) < 100 {
					rows = append(rows, n)
				}
			}
		}
		response.Success(c, gin.H{"items": rows, "unread": unread})
	}
}
func handleInboxRead(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var n InboxNotice
		if db.Where("id = ? AND user_id = ?", c.Param("id"), currentUserID(c)).First(&n).Error != nil || !noticeVisible(db, &n, currentUserID(c)) {
			response.ErrorNotFound(c, "消息不存在")
			return
		}
		now := businessNow()
		if err := db.Model(&n).Update("read_at", now).Error; err != nil {
			response.ErrorInternal(c, "保存失败")
			return
		}
		response.Success(c, gin.H{"read": true})
	}
}
