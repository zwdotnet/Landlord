package rental

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"smallgo/server/audit"
	"smallgo/server/response"
	"smallgo/server/sysconfig"
	"smallgo/server/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type billIssueTenant struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	MoveInDate   string `json:"move_in_date"`
	MoveOutDate  string `json:"move_out_date"`
	LeaseEndDate string `json:"lease_end_date"`
	Active       bool   `json:"active"`
}

func handleBillIssueRooms(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		page, size := utils.NormalizePage(utils.Atoi(c.Query("page"), 1), utils.Atoi(c.Query("pageSize"), 10))
		q, empty := scopeLedger(db.Model(&Room{}), c)
		if empty {
			response.SuccessPage(c, []meterRoom{}, 0, page, size)
			return
		}
		if word := strings.TrimSpace(c.Query("keyword")); word != "" {
			q = q.Where("(room_no LIKE ? OR community LIKE ? OR building LIKE ?)", "%"+word+"%", "%"+word+"%", "%"+word+"%")
		}
		var total int64
		rooms := make([]meterRoom, 0)
		if q.Count(&total).Error != nil || q.Select("id,room_no,community,building").Order("id ASC").Offset(utils.Offset(page, size)).Limit(size).Find(&rooms).Error != nil {
			response.ErrorInternal(c, "读取开单房源失败，请重试")
			return
		}
		response.SuccessPage(c, rooms, total, page, size)
	}
}

func issueSelection(c *gin.Context) (uint, string, []uint, error) {
	id, err := strconv.ParseUint(c.Query("room_id"), 10, 64)
	period := strings.TrimSpace(c.Query("period"))
	if err != nil || id == 0 || !isValidPeriod(period) {
		return 0, "", nil, errors.New("请选择房源与正确账期")
	}
	var ids []uint
	if raw := c.Query("tenant_ids"); raw != "" {
		parts := strings.Split(raw, ",")
		if len(parts) > 50 {
			return 0, "", nil, errors.New("一次最多选择50条入住记录")
		}
		seen := map[uint]bool{}
		for _, part := range parts {
			v, e := strconv.ParseUint(part, 10, 64)
			if e != nil || v == 0 || seen[uint(v)] {
				return 0, "", nil, errors.New("请选择有效且不重复的入住记录")
			}
			ids = append(ids, uint(v))
			seen[uint(v)] = true
		}
	}
	return uint(id), period, ids, nil
}

func issueTenants(db *gorm.DB, room *Room, period string) ([]Tenant, error) {
	var rows []Tenant
	if err := db.Where("org_id = ? AND room_id = ?", room.LedgerID, room.ID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	eligible := make([]Tenant, 0)
	for _, t := range rows {
		if start := periodOfDate(t.MoveInDate); start != "" && start > period {
			continue
		}
		if end := periodOfDate(t.MoveOutDate); end != "" && end < period {
			continue
		}
		if end := periodOfDate(t.LeaseEndDate); end != "" && end < period {
			continue
		}
		eligible = append(eligible, t)
	}
	return eligible, nil
}

func handleBillIssueTenants(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		id, period, _, err := issueSelection(c)
		if err != nil {
			response.ErrorBadRequest(c, err.Error())
			return
		}
		room, ok := loadRoom(db, c, id)
		if !ok {
			return
		}
		tenants, err := issueTenants(db, room, period)
		if err != nil {
			response.ErrorInternal(c, "读取入住记录失败，请重试")
			return
		}
		rows := make([]billIssueTenant, 0, len(tenants))
		for _, t := range tenants {
			rows = append(rows, billIssueTenant{t.ID, t.Name, t.MoveInDate, t.MoveOutDate, t.LeaseEndDate, t.Active})
		}
		response.Success(c, gin.H{"room": gin.H{"id": room.ID, "room_no": room.RoomNo}, "period": period, "tenants": rows})
	}
}

type issueClaims struct {
	User    uint   `json:"user"`
	Ledger  uint   `json:"ledger"`
	Room    uint   `json:"room"`
	Period  string `json:"period"`
	Tenants []uint `json:"tenants"`
	Digest  string `json:"digest"`
	Expires int64  `json:"expires"`
}

func issueToken(claims issueClaims, secret string) (string, error) {
	if secret == "" {
		return "", errors.New("missing signing configuration")
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func parseIssueToken(value, secret string) (issueClaims, error) {
	var claims issueClaims
	parts := strings.Split(value, ".")
	if secret == "" || len(value) > 8192 || len(parts) != 2 {
		return claims, errors.New("invalid preview")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return claims, errors.New("invalid preview")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims, err
	}
	if json.Unmarshal(raw, &claims) != nil || claims.Room == 0 || len(claims.Tenants) == 0 || len(claims.Tenants) > 50 || !isValidPeriod(claims.Period) || claims.Expires <= time.Now().Unix() {
		return issueClaims{}, errors.New("expired preview")
	}
	return claims, nil
}

type billIssueView struct {
	Exists        bool    `json:"exists"`
	ExistingMatch bool    `json:"existing_match"`
	Bill          *Bill   `json:"bill"`
	Items         []gin.H `json:"items"`
	CanConfirm    bool    `json:"can_confirm"`
	BlockReason   string  `json:"block_reason"`
	Token         string  `json:"confirmation_token,omitempty"`
	digest        string
	customs       []BillItem
}

func overlappingIssue(db *gorm.DB, room *Room, period string, ids []uint) (*Bill, error) {
	var rows []Bill
	if err := db.Where("org_id = ? AND room_id = ? AND period = ?", room.LedgerID, room.ID, period).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, b := range rows {
		if b.TenancyKey == "" || b.TenancyKey == tenantKey(ids) {
			return &b, nil
		}
		for _, recipient := range b.RecipientTenantIDs {
			for _, id := range ids {
				if recipient == id {
					return &b, nil
				}
			}
		}
	}
	return nil, nil
}

func issueBlockReason(db *gorm.DB, room *Room, bill *Bill, tenants []Tenant, period string) (string, error) {
	if bill.RequiresManualRent {
		return "本期租金条款需要人工核对，请在Web处理", nil
	}
	first, _ := time.ParseInLocation("2006-01", period, businessZone)
	last := first.AddDate(0, 1, -1)
	start, end := first, last
	for _, t := range tenants {
		move, ok := parseLeaseDate(t.MoveInDate)
		if !ok {
			return "入住日期缺失或无效，请先在Web补齐", nil
		}
		if !t.Active {
			return "退租结算请在Web核对，本页可查看已有账单", nil
		}
		if move.After(start) {
			start = move
		}
		for _, date := range []string{t.MoveOutDate, t.LeaseEndDate} {
			if v, ok := parseLeaseDate(date); ok && v.Before(end) {
				end = v
			}
		}
	}
	if start.After(end) {
		return "所选入住记录不属于同一有效入住区间，请分别预览", nil
	}
	// A same-month successor needs a dated final bill to establish the boundary.
	all, err := issueTenants(db, room, period)
	if err != nil {
		return "", err
	}
	for _, selected := range tenants {
		if periodOfDate(selected.MoveInDate) != period {
			continue
		}
		for _, old := range all {
			if old.ID == selected.ID || old.MoveOutDate == "" || old.MoveOutDate >= selected.MoveInDate {
				continue
			}
			previous, err := overlappingIssue(db, room, period, []uint{old.ID})
			if err != nil {
				return "", err
			}
			if previous == nil {
				return "同月前任租客尚未完成结算，请先在Web核对", nil
			}
		}
	}
	excluded := excludedFeeSet(append(append([]string{}, bill.ExcludedFees...), bill.DisabledFees...))
	required := (normalizeWaterMode(bill.WaterMode) == waterModeMeter && !excluded[feeKeyWater]) || !excluded[feeKeyElec] || !excluded[feeKeyGas]
	var reading MeterRecord
	if required {
		err := db.Where("org_id = ? AND room_id = ? AND period = ?", room.LedgerID, room.ID, period).First(&reading).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && reading.Note == "随账单生成") {
			return "本期尚未确认抄表，请先补齐读数后重新预览", nil
		}
		if err != nil {
			return "", err
		}
	}
	if (normalizeWaterMode(bill.WaterMode) == waterModeMeter && !excluded[feeKeyWater] && bill.WaterNow < bill.WaterLast) || (!excluded[feeKeyElec] && bill.ElecNow < bill.ElecLast) || (!excluded[feeKeyGas] && bill.GasNow < bill.GasLast) {
		return "本期读数低于上期，请在Web核对换表或录入情况", nil
	}
	return "", nil
}

func buildBillIssue(db *gorm.DB, c *gin.Context, room *Room, period string, ids []uint) (*billIssueView, error) {
	if len(ids) == 0 {
		return nil, errors.New("请选择本期承租关系")
	}
	tenants, err := billTenants(db, room, period, ids)
	if err != nil {
		return nil, err
	}
	for _, t := range tenants {
		if end := periodOfDate(t.LeaseEndDate); end != "" && period > end {
			return nil, errors.New("账期晚于租约结束月份，请在Web核对")
		}
	}
	existing, err := overlappingIssue(db, room, period, ids)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		fillLegacyRentBasis(db, existing)
		match := existing.TenancyKey == tenantKey(ids)
		reason := "该承租关系本期已出账，请核对原单"
		if !match {
			reason = "本期已有归属待核对或包含所选租客的账单，请在Web核对原单"
		}
		return &billIssueView{Exists: true, ExistingMatch: match, Bill: existing, Items: billItemRows(db, c, existing), BlockReason: reason}, nil
	}
	bill := newBillForTenants(db, room, period, tenants)
	customs := applyCustomFees(db, room, bill)
	bill.recalc()
	items := billDraftItemRows(db, room.LedgerID, append(billBuiltInItems(*bill), customs...))
	reason, err := issueBlockReason(db, room, bill, tenants, period)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(struct {
		Bill  *Bill
		Items []gin.H
	}{bill, items})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return &billIssueView{Bill: bill, Items: items, CanConfirm: reason == "", BlockReason: reason, digest: hex.EncodeToString(sum[:]), customs: customs}, nil
}

func handleBillIssuePreview(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		id, period, ids, err := issueSelection(c)
		if err != nil || len(ids) == 0 {
			response.ErrorBadRequest(c, "请选择房源、账期及承租关系")
			return
		}
		var view *billIssueView
		denied := false
		err = db.Transaction(func(tx *gorm.DB) error {
			room, ok := loadRoom(tx, c, id)
			if !ok {
				denied = true
				return errors.New("room unavailable")
			}
			var err error
			view, err = buildBillIssue(tx, c, room, period, ids)
			if err != nil {
				return err
			}
			if view.CanConfirm && permRank[ledgerPerms(tx, currentUserID(c), room.LedgerID)[ModuleBills]] < permRank[PermEdit] {
				view.CanConfirm = false
				view.BlockReason = "当前台账只有账单查看权限"
			}
			if view.CanConfirm {
				secret, err := sysconfig.GetConfig(tx, "jwt_secret", 0)
				if err != nil {
					return err
				}
				view.Token, err = issueToken(issueClaims{User: currentUserID(c), Ledger: room.LedgerID, Room: room.ID, Period: period, Tenants: view.Bill.RecipientTenantIDs, Digest: view.digest, Expires: time.Now().Add(10 * time.Minute).Unix()}, secret)
				return err
			}
			return nil
		})
		if denied {
			return
		}
		if err != nil {
			response.ErrorBadRequest(c, "无法完成账单预览，请核对入住关系和账期后重试")
			return
		}
		response.Success(c, view)
	}
}

var errIssueStale = errors.New("预览已变化，请重新预览后再确认出账")

func handleBillConfirm(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		var body struct {
			Token string `json:"confirmation_token"`
		}
		if c.ShouldBindJSON(&body) != nil {
			response.ErrorBadRequest(c, "请先完成账单预览")
			return
		}
		secret, _ := sysconfig.GetConfig(db, "jwt_secret", 0)
		claims, err := parseIssueToken(body.Token, secret)
		if err != nil {
			response.Error(c, http.StatusConflict, http.StatusConflict, "预览无效或已过期，请重新预览")
			return
		}
		ledger, ok := writeLedgerID(c)
		if !ok || ledger != claims.Ledger || claims.User != currentUserID(c) {
			response.ErrorForbidden(c, "预览不属于当前账号或台账，请重新预览")
			return
		}
		created, denied := false, false
		var view *billIssueView
		err = db.Transaction(func(tx *gorm.DB) error {
			lock := tx.Model(&Room{}).Where("id = ? AND org_id = ?", claims.Room, ledger).UpdateColumn("id", gorm.Expr("id"))
			if lock.Error != nil {
				return lock.Error
			}
			if lock.RowsAffected != 1 {
				return errIssueStale
			}
			room, ok := loadRoom(tx, c, claims.Room)
			if !ok {
				denied = true
				return errIssueStale
			}
			if !requireLedgerWrite(tx, c, room.LedgerID, ModuleBills, PermEdit) {
				denied = true
				return errIssueStale
			}
			var err error
			view, err = buildBillIssue(tx, c, room, claims.Period, claims.Tenants)
			if err != nil {
				return errIssueStale
			}
			if view.Exists {
				return nil
			}
			if !view.CanConfirm || view.digest != claims.Digest {
				return errIssueStale
			}
			view.Bill.UserID = currentUserID(c)
			if err := tx.Create(view.Bill).Error; err != nil {
				return err
			}
			view.Bill.ReceiptNo = receiptNo(view.Bill.Period, view.Bill.RoomNo, view.Bill.ID)
			if err := tx.Model(view.Bill).UpdateColumn("receipt_no", view.Bill.ReceiptNo).Error; err != nil {
				return err
			}
			if err := syncBillItems(tx, view.Bill, view.customs); err != nil {
				return err
			}
			if err := settleMeterRecord(tx, view.Bill); err != nil {
				return err
			}
			created = true
			return nil
		})
		if denied {
			return
		}
		if err != nil {
			if errors.Is(err, errIssueStale) {
				response.Error(c, http.StatusConflict, http.StatusConflict, err.Error())
			} else {
				response.ErrorInternal(c, "出账未确认，请先核对是否已有账单")
			}
			return
		}
		if created {
			audit.Log(db, c, "bill_confirm", "bill", view.Bill.ID, "通过账单预览确认出账")
		} else {
			c.Set("audit_logged", true)
		}
		response.Success(c, gin.H{"created": created, "existing_match": created || view.ExistingMatch, "bill": view.Bill, "message": view.BlockReason})
	}
}
