package rental

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"

	"smallgo/server/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// A Tenant row identifies one stay, even when successive renters share a name.
// Names are display snapshots only; they never grant portal access.
func tenantKey(ids []uint) string {
	if len(ids) == 0 {
		return ""
	}
	copyIDs := append([]uint(nil), ids...)
	sort.Slice(copyIDs, func(i, j int) bool { return copyIDs[i] < copyIDs[j] })
	parts := make([]string, 0, len(copyIDs))
	for _, id := range copyIDs {
		parts = append(parts, strconv.FormatUint(uint64(id), 10))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return hex.EncodeToString(sum[:])
}

// Explicit targets also allow a landlord to issue a departed renter's final
// bill after another renter has moved into the same room in the same month.
func billTenants(db *gorm.DB, room *Room, period string, ids []uint) ([]Tenant, error) {
	q := db.Where("org_id = ? AND room_id = ?", room.LedgerID, room.ID)
	if len(ids) == 0 {
		q = q.Where("active = ?", true)
	} else {
		q = q.Where("id IN ?", ids)
	}
	var tenants []Tenant
	if err := q.Order("id ASC").Find(&tenants).Error; err != nil {
		return nil, err
	}
	if len(ids) > 0 && len(tenants) != len(ids) {
		return nil, errors.New("租客不存在、重复或不属于该房源")
	}
	if len(ids) == 0 {
		eligible := tenants[:0]
		for _, tenant := range tenants {
			if from := periodOfDate(tenant.MoveInDate); from != "" && period < from {
				continue
			}
			if to := periodOfDate(tenant.MoveOutDate); to != "" && period > to {
				continue
			}
			eligible = append(eligible, tenant)
		}
		tenants = eligible
	}
	// Explicit historical targets must overlap the bill's month. Default active
	// targets retain rental's manual historical-entry workflow.
	if len(ids) > 0 {
		for _, t := range tenants {
			if from := periodOfDate(t.MoveInDate); from != "" && period < from {
				return nil, errors.New("账期早于租客入住月份")
			}
			if to := periodOfDate(t.MoveOutDate); to != "" && period > to {
				return nil, errors.New("账期晚于租客退租月份")
			}
		}
	}
	return tenants, nil
}

func setBillRecipients(b *Bill, tenants []Tenant) {
	b.RecipientTenantIDs = make([]uint, 0, len(tenants))
	names := make([]string, 0, len(tenants))
	for _, t := range tenants {
		b.RecipientTenantIDs = append(b.RecipientTenantIDs, t.ID)
		names = append(names, t.Name)
	}
	b.TenancyKey = tenantKey(b.RecipientTenantIDs)
	b.TenantName = strings.Join(names, "、")
}

// On a dated same-month turnover, the predecessor's final billed reading is
// the new tenancy's starting reading. A later tenancy must never feed an
// earlier tenancy's final bill.
func precedingTurnoverBill(db *gorm.DB, room *Room, tenants []Tenant, period string) (Bill, bool) {
	start := ""
	for _, tenant := range tenants {
		if periodOfDate(tenant.MoveInDate) != period {
			return Bill{}, false
		}
		if start == "" || tenant.MoveInDate < start {
			start = tenant.MoveInDate
		}
	}
	if start == "" {
		return Bill{}, false
	}
	var bills []Bill
	if db.Where("org_id = ? AND room_id = ? AND period = ?", room.LedgerID, room.ID, period).
		Order("id DESC").Find(&bills).Error != nil {
		return Bill{}, false
	}
	for _, bill := range bills {
		if len(bill.RecipientTenantIDs) == 0 {
			continue
		}
		var previous []Tenant
		if db.Where("org_id = ? AND room_id = ? AND id IN ?", room.LedgerID, room.ID, bill.RecipientTenantIDs).Find(&previous).Error != nil || len(previous) != len(bill.RecipientTenantIDs) {
			continue
		}
		before := true
		for _, tenant := range previous {
			if _, ok := parseLeaseDate(tenant.MoveOutDate); !ok || tenant.MoveOutDate >= start {
				before = false
			}
		}
		if before {
			return bill, true
		}
	}
	return Bill{}, false
}

func sameTenancyBill(db *gorm.DB, b *Bill) *gorm.DB {
	return db.Where("org_id = ? AND room_id = ? AND period = ? AND (tenancy_key = ? OR tenancy_key = '')",
		b.LedgerID, b.RoomID, b.Period, b.TenancyKey)
}

func tenancyFixedItemDue(db *gorm.DB, room *Room, period, cycle string, tenants []Tenant) bool {
	months := cycleMonths(cycle)
	if months <= 1 {
		return true
	}
	b := &Bill{}
	setBillRecipients(b, tenants)
	var last Bill
	err := db.Where("org_id = ? AND room_id = ? AND tenancy_key = ? AND period < ? AND (rent > 0 OR sanitation_fee > 0 OR management_fee > 0)",
		room.LedgerID, room.ID, b.TenancyKey, period).Order("period DESC").First(&last).Error
	return err != nil || anchorIntervalDue(last.Period, period, months)
}

// Additive upgrade: legacy bills remain visible to landlords, but are not
// assigned to renters from names or month ranges. No business amounts change.
func migrateBillOwnership(db *gorm.DB) error {
	for _, idx := range []string{"idx_rental_bill_room_period", "idx_rental_bill_org_period", "idx_rental_bill_ledger_period"} {
		if err := db.Exec("DROP INDEX IF EXISTS " + idx).Error; err != nil {
			return err
		}
	}
	return db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_landlord_bill_tenancy_period ON bills(org_id, room_id, period, tenancy_key)").Error
}

// Legacy assignment is a deliberate landlord action, never a name-based
// migration. Once assigned, recipients cannot be changed by this endpoint.
func handleLegacyBillRecipients(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		bill, ok := findUserBill(db, c)
		if !ok || !requireLedgerWrite(db, c, bill.LedgerID, ModuleBills, PermEdit) {
			return
		}
		var req struct {
			TenantIDs []uint `json:"tenant_ids"`
		}
		if c.ShouldBindJSON(&req) != nil || len(req.TenantIDs) == 0 {
			response.ErrorBadRequest(c, "请选择账单所属租客")
			return
		}
		if len(bill.RecipientTenantIDs) != 0 || bill.TenancyKey != "" {
			response.ErrorBadRequest(c, "已明确归属的账单不能重新分配")
			return
		}
		var room Room
		if db.Where("id = ? AND org_id = ?", bill.RoomID, bill.LedgerID).First(&room).Error != nil {
			response.ErrorNotFound(c, "房源不存在")
			return
		}
		tenants, err := billTenants(db, &room, bill.Period, req.TenantIDs)
		if err != nil {
			response.ErrorBadRequest(c, err.Error())
			return
		}
		oldName := bill.TenantName
		setBillRecipients(bill, tenants)
		bill.TenantName = oldName
		result := db.Model(&Bill{}).Where("id = ? AND tenancy_key = ''", bill.ID).
			Select("RecipientTenantIDs", "TenancyKey").Updates(bill)
		if result.Error != nil || result.RowsAffected != 1 {
			response.ErrorBadRequest(c, "账单归属保存失败，请刷新后核对")
			return
		}
		response.Success(c, bill)
	}
}
