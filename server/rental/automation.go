package rental

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"smallgo/server/apps"
	"smallgo/server/database"
	"smallgo/server/logger"
	"smallgo/server/response"
	"smallgo/server/scheduler"
)

// Plans are opt-in and tied to an explicit stay, so a replacement renter never
// inherits the former renter's automation or charges.
type AutomationPlan struct {
	ID            uint      `gorm:"primarykey" json:"id"`
	LedgerID      uint      `gorm:"column:org_id;index;uniqueIndex:idx_auto_stay" json:"ledger_id"`
	RoomID        uint      `gorm:"uniqueIndex:idx_auto_stay" json:"room_id"`
	TenancyKey    string    `gorm:"uniqueIndex:idx_auto_stay" json:"-"`
	TenantIDs     []uint    `gorm:"serializer:json" json:"tenant_ids"`
	RoomNo        string    `json:"room_no"`
	Enabled       bool      `json:"enabled"`
	MeterDay      int       `json:"meter_day"`
	BillDay       int       `json:"bill_day"`
	DueDay        int       `json:"due_day"`
	LeadDays      int       `json:"lead_days"`
	StartPeriod   string    `json:"start_period"`
	TermsDigest   string    `json:"-"`
	TermsSnapshot string    `json:"-"`
	Version       int       `json:"version"`
	CreatedBy     uint      `json:"-"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CycleTask struct {
	ID            uint      `gorm:"primarykey" json:"id"`
	LedgerID      uint      `gorm:"column:org_id;index" json:"ledger_id"`
	PlanID        uint      `gorm:"uniqueIndex:idx_cycle_plan_period" json:"plan_id"`
	Period        string    `gorm:"uniqueIndex:idx_cycle_plan_period" json:"period"`
	RoomID        uint      `gorm:"index" json:"room_id"`
	RoomNo        string    `json:"room_no"`
	PlanVersion   int       `json:"plan_version"`
	MeterDate     string    `json:"meter_date"`
	BillDate      string    `json:"bill_date"`
	DueDate       string    `json:"due_date"`
	Status        string    `gorm:"index" json:"status"`
	Detail        string    `json:"detail"`
	BillID        uint      `json:"bill_id"`
	Attempts      int       `json:"attempts"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

var businessZone = time.FixedZone("Asia/Hong_Kong", 8*3600)

func businessNow() time.Time { return time.Now().In(businessZone) }
func cycleDate(period string, day int) time.Time {
	p, _ := time.ParseInLocation("2006-01", period, businessZone)
	return time.Date(p.Year(), p.Month(), day, 8, 0, 0, 0, businessZone)
}

func init() {
	database.RegisterModels(&AutomationPlan{}, &CycleTask{})
	scheduler.Register(scheduler.Job{Name: "landlord-automation", Interval: time.Minute, RunAtStart: true, Run: func() {
		if appDB != nil {
			if err := runAutomation(appDB, businessNow(), nil); err != nil {
				logger.Error("automation: %v", err)
			}
			dispatchWebPush(appDB, businessNow())
		}
	}})
	apps.Register(apps.App{Name: "landlord_automation", DisplayName: "自动出账与提醒", SetupAuth: func(auth *gin.RouterGroup, db *gorm.DB) {
		g := auth.Group("", approvalGate(db), resolveLedger(db), denyTenantRole())
		g.GET("/rental/automation/plans", requireModule(db, ModuleBills, PermView), handleAutomationPlans(db))
		g.POST("/rental/automation/plans", requireLedgerModule(db, ModuleBills, PermEdit), handleAutomationSave(db))
		g.GET("/rental/automation/tasks", requireModule(db, ModuleBills, PermView), handleCycleTasks(db))
		g.POST("/rental/automation/run", requireLedgerModule(db, ModuleBills, PermEdit), func(c *gin.Context) {
			ledger, ok := writeLedgerID(c)
			if !ok {
				return
			}
			if !requireLedgerWrite(db, c, ledger, ModuleMeters, PermEdit) {
				return
			}
			if err := runAutomation(db, businessNow(), []uint{ledger}); err != nil {
				response.ErrorInternal(c, "检查失败，请稍后重试")
				return
			}
			response.Success(c, gin.H{"checked": true})
		})
	}})
}

// The snapshot intentionally includes dated renewal records and fee switches.
// Changed terms need the landlord to confirm the plan, rather than silently
// charging a different contract. Contact-only edits do not affect the digest.
func automationTerms(db *gorm.DB, room *Room, tenants []Tenant) (string, string, error) {
	var terms []map[string]interface{}
	for _, t := range tenants {
		var renewals []LeaseRenewal
		if err := db.Where("org_id = ? AND tenant_id = ?", room.LedgerID, t.ID).Order("id").Find(&renewals).Error; err != nil {
			return "", "", err
		}
		cfg := effectiveTenantsBilling(db, room, []Tenant{t})
		terms = append(terms, map[string]interface{}{"id": t.ID, "from": t.MoveInDate, "end": t.LeaseEndDate, "out": t.MoveOutDate, "deposit": t.Deposit, "water_mode": cfg.WaterMode, "water_amount": cfg.WaterAmount, "cycle": cfg.PayCycle, "excluded": t.ExcludedFees, "renewals": renewals})
	}
	ensureFeeItems(db, room.LedgerID)
	var fees []FeeItem
	if err := db.Where("org_id = ?", room.LedgerID).Order("id").Find(&fees).Error; err != nil {
		return "", "", err
	}
	water, elec, gas := defaultPrices(db)
	raw, err := json.Marshal(map[string]interface{}{"tenants": terms, "rent": room.DefaultRent, "sanitation": room.DefaultSanitationFee, "management": room.DefaultManagementFee, "elec": room.ElecPrice, "gas": room.GasPrice, "defaults": []float64{water, elec, gas}, "fees": fees})
	if err != nil {
		return "", "", err
	}
	h := sha256.Sum256(raw)
	return string(raw), hex.EncodeToString(h[:]), nil
}
func planTenants(db *gorm.DB, p *AutomationPlan) (Room, []Tenant, error) {
	var room Room
	var tenants []Tenant
	if err := db.Where("id = ? AND org_id = ?", p.RoomID, p.LedgerID).First(&room).Error; err != nil {
		return room, nil, err
	}
	if err := db.Where("id IN ? AND room_id = ? AND org_id = ?", p.TenantIDs, p.RoomID, p.LedgerID).Order("id").Find(&tenants).Error; err != nil {
		return room, nil, err
	}
	if len(tenants) != len(p.TenantIDs) || len(tenants) == 0 {
		return room, nil, errors.New("入住关系已变化")
	}
	return room, tenants, nil
}
func handleAutomationSave(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			RoomID      uint   `json:"room_id"`
			TenantIDs   []uint `json:"tenant_ids"`
			Enabled     bool   `json:"enabled"`
			MeterDay    int    `json:"meter_day"`
			BillDay     int    `json:"bill_day"`
			DueDay      int    `json:"due_day"`
			LeadDays    *int   `json:"lead_days"`
			StartPeriod string `json:"start_period"`
		}
		if c.ShouldBindJSON(&req) != nil {
			response.ErrorBadRequest(c, "参数错误")
			return
		}
		room, ok := loadRoom(db, c, req.RoomID)
		if !ok {
			return
		}
		if !requireLedgerWrite(db, c, room.LedgerID, ModuleBills, PermEdit) || !requireLedgerWrite(db, c, room.LedgerID, ModuleMeters, PermEdit) {
			return
		}
		now := businessNow()
		if req.StartPeriod == "" {
			req.StartPeriod = now.Format("2006-01")
		}
		if !isValidPeriod(req.StartPeriod) || req.StartPeriod < "2000-01" || req.StartPeriod > now.AddDate(5, 0, 0).Format("2006-01") {
			response.ErrorBadRequest(c, "请选择有效的开始账期")
			return
		}
		tenants, err := billTenants(db, room, req.StartPeriod, req.TenantIDs)
		if err != nil || len(tenants) == 0 {
			response.ErrorBadRequest(c, "请选择该房源有效入住关系")
			return
		}
		cfg := effectiveTenantsBilling(db, room, tenants)
		if req.DueDay == 0 {
			req.DueDay = cfg.PayDay
			if req.DueDay == 0 {
				req.DueDay = 10
			}
		}
		if req.BillDay == 0 {
			req.BillDay = req.DueDay - 3
			if req.BillDay < 1 {
				req.BillDay = 1
			}
		}
		if req.MeterDay == 0 {
			req.MeterDay = req.BillDay
		}
		lead := 3
		if req.LeadDays != nil {
			lead = *req.LeadDays
		}
		if req.MeterDay < 1 || req.MeterDay > 28 || req.BillDay < req.MeterDay || req.BillDay > 28 || req.DueDay < req.BillDay || req.DueDay > 28 || lead < 0 || lead > 30 {
			response.ErrorBadRequest(c, "日期需为 1 至 28 日，抄表日不晚于出账日，出账日不晚于缴费日；提前天数为 0 至 30")
			return
		}
		for _, t := range tenants {
			if _, ok := parseLeaseDate(t.MoveInDate); !ok {
				response.ErrorBadRequest(c, "请先补齐入住日期")
				return
			}
			if _, ok := parseLeaseDate(t.LeaseEndDate); !ok {
				response.ErrorBadRequest(c, "请先补齐合同到期日")
				return
			}
		}
		tmp := &Bill{}
		setBillRecipients(tmp, tenants)
		snapshot, digest, err := automationTerms(db, room, tenants)
		if err != nil {
			response.ErrorInternal(c, "读取合同条款失败")
			return
		}
		var p AutomationPlan
		err = db.Transaction(func(tx *gorm.DB) error {
			result := tx.Where("org_id = ? AND room_id = ? AND tenancy_key = ?", room.LedgerID, room.ID, tmp.TenancyKey).First(&p)
			if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return result.Error
			}
			if p.ID > 0 && req.StartPeriod != p.StartPeriod {
				var n int64
				tx.Model(&CycleTask{}).Where("plan_id = ?", p.ID).Count(&n)
				if n > 0 {
					return errors.New("已有任务的计划不能改开始账期，请保持原账期")
				}
			}
			p.LedgerID = room.LedgerID
			p.RoomID = room.ID
			p.RoomNo = room.RoomNo
			p.TenantIDs = tmp.RecipientTenantIDs
			p.TenancyKey = tmp.TenancyKey
			p.Enabled = req.Enabled
			p.MeterDay = req.MeterDay
			p.BillDay = req.BillDay
			p.DueDay = req.DueDay
			p.LeadDays = lead
			p.StartPeriod = req.StartPeriod
			p.TermsSnapshot = snapshot
			p.TermsDigest = digest
			p.Version++
			p.CreatedBy = currentUserID(c)
			if p.Enabled {
				var others []AutomationPlan
				if err := tx.Where("org_id = ? AND room_id = ? AND enabled = ? AND tenancy_key <> ?", p.LedgerID, p.RoomID, true, p.TenancyKey).Find(&others).Error; err != nil {
					return err
				}
				for _, other := range others {
					_, oldTenants, err := planTenants(tx, &other)
					if err != nil {
						return errors.New("原计划入住关系待核对，请先暂停原计划")
					}
					for _, a := range tenants {
						for _, b := range oldTenants {
							aEnd, bEnd := a.LeaseEndDate, b.LeaseEndDate
							if a.MoveOutDate != "" && a.MoveOutDate < aEnd {
								aEnd = a.MoveOutDate
							}
							if b.MoveOutDate != "" && b.MoveOutDate < bEnd {
								bEnd = b.MoveOutDate
							}
							if a.MoveInDate <= bEnd && b.MoveInDate <= aEnd {
								return errors.New("该房源存在租期重叠的启用计划，请先暂停原计划，避免重复计费")
							}
						}
					}
				}
			}
			return tx.Save(&p).Error
		})
		if err != nil {
			response.ErrorBadRequest(c, err.Error())
			return
		}
		response.Success(c, p)
	}
}
func handleAutomationPlans(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		q, empty := scopeLedger(db.Model(&AutomationPlan{}), c)
		if empty {
			response.Success(c, []AutomationPlan{})
			return
		}
		if id := c.Query("room_id"); id != "" {
			q = q.Where("room_id = ?", id)
		}
		var rows []AutomationPlan
		if q.Order("id DESC").Limit(300).Find(&rows).Error != nil {
			response.ErrorInternal(c, "读取失败")
			return
		}
		response.Success(c, rows)
	}
}
func handleCycleTasks(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		q, empty := scopeLedger(db.Model(&CycleTask{}), c)
		if empty {
			response.Success(c, []CycleTask{})
			return
		}
		if period := c.Query("period"); period != "" {
			q = q.Where("period = ?", period)
		}
		var rows []CycleTask
		if q.Order("period DESC,id DESC").Limit(300).Find(&rows).Error != nil {
			response.ErrorInternal(c, "读取失败")
			return
		}
		response.Success(c, rows)
	}
}

func runAutomation(db *gorm.DB, now time.Time, ledgerIDs []uint) error {
	now = now.In(businessZone)
	var plans []AutomationPlan
	q := db.Where("enabled = ?", true)
	if ledgerIDs != nil {
		if len(ledgerIDs) == 0 {
			return nil
		}
		q = q.Where("org_id IN ?", ledgerIDs)
	}
	if err := q.Order("id").Find(&plans).Error; err != nil {
		return err
	}
	for _, p := range plans {
		start, _ := time.ParseInLocation("2006-01", p.StartPeriod, businessZone)
		if start.IsZero() {
			continue
		}
		// A bounded pass catches up oldest months first. Subsequent ticks continue
		// after terminal tasks, without losing months during long downtime.
		scanned := 0
		for month := start; month.Format("2006-01") <= now.AddDate(0, 1, 0).Format("2006-01"); month = month.AddDate(0, 1, 0) {
			period := month.Format("2006-01")
			var existing CycleTask
			if db.Where("plan_id = ? AND period = ?", p.ID, period).First(&existing).Error == nil && (existing.Status == "issued" || existing.Status == "cancelled") {
				continue
			}
			if scanned >= 120 {
				break
			}
			scanned++
			if cycleDate(period, p.MeterDay).AddDate(0, 0, -p.LeadDays).After(now) {
				continue
			}
			task := CycleTask{LedgerID: p.LedgerID, PlanID: p.ID, Period: period, RoomID: p.RoomID, RoomNo: p.RoomNo, Status: "pending"}
			if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&task).Error; err != nil {
				return err
			}
			if err := processCycleTask(db, p.ID, period, now); err != nil {
				next := now.Add(5 * time.Minute)
				db.Model(&CycleTask{}).Where("plan_id = ? AND period = ? AND status NOT IN ?", p.ID, period, []string{"issued", "cancelled"}).Updates(map[string]interface{}{"status": "retry", "detail": "出账暂时失败，将自动重试", "next_attempt_at": next, "attempts": gorm.Expr("attempts + 1")})
				logger.Error("automation plan %d period %s: %v", p.ID, period, err)
			}
		}
	}
	return nil
}
func processCycleTask(db *gorm.DB, planID uint, period string, now time.Time) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var p AutomationPlan
		var task CycleTask
		if err := tx.First(&p, planID).Error; err != nil {
			return err
		}
		if !p.Enabled {
			return nil
		}
		if err := tx.Where("plan_id = ? AND period = ?", planID, period).First(&task).Error; err != nil {
			return err
		}
		if task.Status == "issued" || task.Status == "cancelled" {
			return nil
		}
		if task.PlanVersion == p.Version && task.NextAttemptAt.After(now) {
			return nil
		}
		task.PlanVersion = p.Version
		task.MeterDate = cycleDate(period, p.MeterDay).Format("2006-01-02")
		task.BillDate = cycleDate(period, p.BillDay).Format("2006-01-02")
		task.DueDate = cycleDate(period, p.DueDay).Format("2006-01-02")
		setState := func(status, detail string) error {
			task.Status = status
			task.Detail = detail
			task.NextAttemptAt = now.Add(time.Minute)
			return tx.Save(&task).Error
		}
		perms := ledgerPerms(tx, p.CreatedBy, p.LedgerID)
		if permRank[perms[ModuleBills]] < permRank[PermEdit] || permRank[perms[ModuleMeters]] < permRank[PermEdit] {
			return setState("review", "计划确认人的权限已变化，请有权限的房东重新保存计划")
		}
		var actor database.User
		if tx.First(&actor, p.CreatedBy).Error != nil || actor.Status != 1 {
			return setState("review", "计划确认账号已停用，请重新确认")
		}
		room, tenants, err := planTenants(tx, &p)
		if err != nil {
			return setState("review", "房源或入住关系已变化，请重新确认计划")
		}
		for _, t := range tenants {
			end := periodOfDate(t.LeaseEndDate)
			out := periodOfDate(t.MoveOutDate)
			from := periodOfDate(t.MoveInDate)
			if (end != "" && period > end) || (out != "" && period > out) || (from != "" && period < from) {
				return setState("cancelled", "该账期不在本入住关系的有效租期内")
			}
		}
		_, digest, err := automationTerms(tx, &room, tenants)
		if err != nil {
			return err
		}
		if digest != p.TermsDigest {
			return setState("review", "租约或计费条款已变化，请重新保存并确认计划")
		}
		probe := &Bill{LedgerID: p.LedgerID, RoomID: p.RoomID, Period: period, TenancyKey: p.TenancyKey}
		var existing Bill
		if err := sameTenancyBill(tx, probe).First(&existing).Error; err == nil {
			if existing.TenancyKey != p.TenancyKey {
				return setState("review", "发现归属未确认的旧账单，请先核对")
			}
			task.BillID = existing.ID
			return setState("issued", "本期已由房东出账，保留原账单")
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var otherBills []Bill
		if err := tx.Where("org_id = ? AND room_id = ? AND period = ?", p.LedgerID, p.RoomID, period).Find(&otherBills).Error; err != nil {
			return err
		}
		for _, other := range otherBills {
			for _, id := range other.RecipientTenantIDs {
				for _, target := range p.TenantIDs {
					if id == target {
						return setState("review", "本期已有包含该承租人的账单，请核对，避免重复计费")
					}
				}
			}
		}
		// Persist the reminder and task in the same transaction. Outbound push is
		// delivered later from a durable queue, never inside the bill transaction.
		if err := notifyMeterTask(tx, &p, &task, now); err != nil {
			return err
		}
		if cycleDate(period, p.BillDay).After(now) {
			return setState("pending", "等待计划出账日")
		}
		var earlier int64
		if err := tx.Model(&CycleTask{}).Where("plan_id = ? AND period < ? AND status NOT IN ?", p.ID, period, []string{"issued", "cancelled"}).Count(&earlier).Error; err != nil {
			return err
		}
		if earlier > 0 {
			return setState("waiting_previous", "等待前期任务完成，以保持押金、收费周期和结转顺序")
		}
		bill := newBillForTenants(tx, &room, period, tenants)
		if bill.RequiresManualRent {
			return setState("review", "租金条款冲突，请房东核对")
		}
		required := false
		excluded := excludedFeeSet(append(append([]string{}, bill.ExcludedFees...), bill.DisabledFees...))
		if bill.WaterMode == waterModeMeter && !excluded[feeKeyWater] {
			required = true
		}
		if !excluded[feeKeyElec] || !excluded[feeKeyGas] {
			required = true
		}
		var reading MeterRecord
		if required && (tx.Where("org_id = ? AND room_id = ? AND period = ?", p.LedgerID, p.RoomID, period).First(&reading).Error != nil || reading.Note == "随账单生成") {
			return setState("waiting_meter", "等待当期抄表，补录后自动继续；不会按零用量出账")
		}
		if (bill.WaterMode == waterModeMeter && !excluded[feeKeyWater] && bill.WaterNow < bill.WaterLast) || (!excluded[feeKeyElec] && bill.ElecNow < bill.ElecLast) || (!excluded[feeKeyGas] && bill.GasNow < bill.GasLast) {
			return setState("review", "本期读数小于上期，请核对换表或输入错误")
		}
		bill.UserID = p.CreatedBy
		customs := applyCustomFees(tx, &room, bill)
		bill.recalc()
		bill.AutomationTaskID = task.ID
		bill.DueDate = task.DueDate
		if err := tx.Create(bill).Error; err != nil {
			return err
		}
		bill.ReceiptNo = receiptNo(period, bill.RoomNo, bill.ID)
		if err := tx.Model(bill).UpdateColumn("receipt_no", bill.ReceiptNo).Error; err != nil {
			return err
		}
		if err := syncBillItems(tx, bill, customs); err != nil {
			return err
		}
		if err := settleMeterRecord(tx, bill); err != nil {
			return err
		}
		task.Attempts++
		task.BillID = bill.ID
		return setState("issued", "已自动出账，租客可查看应缴通知")
	})
}

// Used by the PDF snapshot builder; issued documents keep their original terms.
func automationAgreementLine(db *gorm.DB, ledger, room uint, ids []uint) []documentLine {
	var p AutomationPlan
	if db.Where("org_id = ? AND room_id = ? AND tenancy_key = ? AND enabled = ?", ledger, room, tenantKey(ids), true).First(&p).Error != nil {
		return nil
	}
	return []documentLine{{"抄表及出账", fmt.Sprintf("每月 %d 日抄表，%d 日出账；提前 %d 天提醒，缺抄表等待确认", p.MeterDay, p.BillDay, p.LeadDays)}, {"缴费期限", fmt.Sprintf("每月 %d 日；按约定租金周期收取，水电按月核对", p.DueDay)}}
}

func automationRoomGuard(db *gorm.DB, roomID uint) error {
	var n int64
	if db.Model(&AutomationPlan{}).Where("room_id = ?", roomID).Count(&n).Error == nil && n > 0 {
		return errors.New("该房源已有周期计划，请保留房源及任务历史，不能删除或跨台账移动")
	}
	return nil
}

func parsePositiveID(s string) uint { n, _ := strconv.ParseUint(s, 10, 32); return uint(n) }
