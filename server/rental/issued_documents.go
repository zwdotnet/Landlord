package rental

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"smallgo/server/apps"
	"smallgo/server/database"
	"smallgo/server/response"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type DocumentTemplate struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Kind      string    `gorm:"uniqueIndex:idx_document_template_version" json:"kind"`
	Version   int       `gorm:"uniqueIndex:idx_document_template_version" json:"version"`
	Title     string    `json:"title"`
	Clauses   []string  `gorm:"serializer:json" json:"clauses"`
	CreatedBy uint      `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// The PDF blob is separate from the data snapshot, but inside SQLite so the
// application's existing database backups include the complete issued file.
type IssuedDocument struct {
	ID              uint      `gorm:"primarykey" json:"id"`
	LedgerID        uint      `gorm:"column:org_id;index;uniqueIndex:idx_document_request" json:"ledger_id"`
	RequestKey      string    `gorm:"not null;uniqueIndex:idx_document_request" json:"-"`
	Number          string    `gorm:"uniqueIndex;not null" json:"number"`
	Kind            string    `gorm:"index" json:"kind"`
	TenantID        uint      `gorm:"index" json:"tenant_id"`
	BillID          uint      `gorm:"index" json:"bill_id"`
	PaymentID       uint      `gorm:"index" json:"payment_id"`
	RenewalID       uint      `json:"renewal_id"`
	TemplateID      uint      `json:"template_id"`
	TemplateVersion int       `json:"template_version"`
	Snapshot        string    `json:"-"`
	PDF             []byte    `json:"-"`
	SHA256          string    `json:"sha256"`
	FileSize        int       `json:"file_size"`
	CreatedBy       uint      `json:"-"`
	CreatedAt       time.Time `json:"created_at"`
}

func (*IssuedDocument) BeforeUpdate(*gorm.DB) error { return errors.New("已归档凭证不可改写") }
func (*IssuedDocument) BeforeDelete(*gorm.DB) error { return errors.New("已归档凭证不可删除") }
func (*DocumentTemplate) BeforeUpdate(*gorm.DB) error {
	return errors.New("模板修改必须创建新版本")
}
func (*DocumentTemplate) BeforeDelete(*gorm.DB) error {
	return errors.New("模板历史版本不可删除")
}

type documentLine struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
type documentSnapshot struct {
	Schema          int            `json:"schema"`
	Title           string         `json:"title"`
	Number          string         `json:"number"`
	TemplateVersion int            `json:"template_version"`
	Lines           []documentLine `json:"lines"`
	Clauses         []string       `json:"clauses"`
	Signatures      bool           `json:"signatures"`
	Footer          string         `json:"footer"`
}

func init() {
	database.RegisterModels(&DocumentTemplate{}, &IssuedDocument{})
	database.Upgrades = append(database.Upgrades, database.Upgrade{Version: "0.9.0", Name: "landlord_issued_documents", Upgrade: func(db *gorm.DB) error {
		if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_request ON payments(org_id,request_key) WHERE request_key <> ''").Error; err != nil {
			return err
		}
		if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_reversal ON payments(reversal_of_id) WHERE reversal_of_id IS NOT NULL").Error; err != nil {
			return err
		}
		return seedDocumentTemplates(db)
	}})
	apps.Register(apps.App{Name: "landlord_documents", DisplayName: "合同与实收凭证", SetupAuth: func(auth *gin.RouterGroup, db *gorm.DB) {
		g := auth.Group("", approvalGate(db), resolveLedger(db), denyTenantRole())
		g.GET("/rental/document-templates", requireModule(db, ModuleTenants, PermView), handleDocumentTemplates(db, false))
		g.GET("/rental/tenants/:id/agreements", requireModule(db, ModuleTenants, PermView), handleAgreementList(db))
		g.POST("/rental/tenants/:id/agreements", requireLedgerModule(db, ModuleTenants, PermEdit), handleAgreementIssue(db))
		g.GET("/rental/documents/:id/file", handleIssuedDocumentFile(db, false))
		g.POST("/rental/payments/:id/reverse", requireLedgerModule(db, ModuleBills, PermEdit), handlePaymentReverse(db))
		p := auth.Group("/rental/portal", approvalGate(db), requireTenantRole())
		p.GET("/agreements", handlePortalAgreements(db))
		p.GET("/documents/:id/file", handleIssuedDocumentFile(db, true))
	}, SetupAdmin: func(admin *gin.RouterGroup, db *gorm.DB) {
		admin.GET("/rental/platform/document-templates", handleDocumentTemplates(db, true))
		admin.POST("/rental/platform/document-templates", handleTemplatePublish(db))
	}})
}

func seedDocumentTemplates(db *gorm.DB) error {
	for _, kind := range []string{"lease", "renewal"} {
		var count int64
		if err := db.Model(&DocumentTemplate{}).Where("kind = ?", kind).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			title, clauses := defaultDocumentClauses(kind)
			if err := db.Create(&DocumentTemplate{Kind: kind, Version: 1, Title: title, Clauses: clauses}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func defaultDocumentClauses(kind string) (string, []string) {
	if kind == "renewal" {
		return "房屋租赁续租协议", []string{
			"甲乙双方就上述房屋原租赁合同的续租事宜达成如下协议，原合同其余条款继续有效。",
			"本次续租期限、月租金及缴费周期以上述本次续租信息为准，乙方应按期足额支付。",
			"押金按原合同执行，续租期间不作调整；租期届满且无违约、无欠费时全额退还。",
			"续租期间的各项费用按上述计费约定承担；房屋及原有设施由乙方妥善使用，非自然损耗的损坏由乙方修复或赔偿。",
			"任何一方不再续租的，应提前一个月书面通知对方；乙方需在租期届满前腾空并结清费用。",
			"本协议作为原租赁合同的补充，与原合同具有同等法律效力；未尽事宜由双方协商解决。",
		}
	}
	return "房屋租赁合同", []string{
		"甲方将上述房屋出租给乙方作居住使用，乙方不得擅自改变房屋用途或转租、分租给他人。",
		"租赁期限以上述租期为准。租期届满，乙方需续租的应提前一个月与甲方协商。",
		"月租金、缴费周期及缴费日期以上述约定为准。乙方应于约定日期前付清；逾期超过十五日的，甲方有权解除合同并从押金中扣除相应损失。",
		"乙方于签订本合同时向甲方交付上述押金，租期届满、乙方结清费用并将房屋恢复原状后，甲方应全额退还押金。",
		"租赁期间的水、电、燃气、卫生、管理等费用按上述计费约定结算；明确不收取的项目不向乙方收费。",
		"房屋及附属设施、家具家电（详见双方交付的物品清单）由甲方交付乙方使用，乙方应妥善保管、合理使用；因乙方使用不当造成损坏的，由乙方负责修复或赔偿。",
		"乙方不得擅自装修或改变房屋结构；确需装饰装修的，应事先取得甲方书面同意。",
		"任何一方提前解除合同的，应提前一个月书面通知对方；甲方因自身原因提前收回房屋的，应退还剩余租金并赔偿乙方相应损失。",
		"本合同未尽事宜，由双方协商补充；补充条款与本合同具有同等效力。本合同一式两份，甲乙双方各执一份，签字后生效。",
	}
}

func handleDocumentTemplates(db *gorm.DB, history bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := seedDocumentTemplates(db); err != nil {
			response.ErrorInternal(c, "模板加载失败")
			return
		}
		var rows []DocumentTemplate
		q := db.Order("kind ASC, version DESC")
		if !history {
			q = q.Where("id IN (SELECT MAX(id) FROM document_templates GROUP BY kind)")
		}
		if q.Find(&rows).Error != nil {
			response.ErrorInternal(c, "模板加载失败")
			return
		}
		response.Success(c, rows)
	}
}

func handleTemplatePublish(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Kind    string   `json:"kind"`
			Title   string   `json:"title"`
			Clauses []string `json:"clauses"`
		}
		if c.ShouldBindJSON(&req) != nil || (req.Kind != "lease" && req.Kind != "renewal") || len(strings.TrimSpace(req.Title)) == 0 || len([]rune(req.Title)) > 80 || len(req.Clauses) < 1 || len(req.Clauses) > 30 {
			response.ErrorBadRequest(c, "请填写模板类型、标题和 1 至 30 条正文")
			return
		}
		for i, line := range req.Clauses {
			req.Clauses[i] = strings.TrimSpace(line)
			if len([]rune(req.Clauses[i])) == 0 || len([]rune(line)) > 2000 {
				response.ErrorBadRequest(c, "每条正文需为 1 至 2000 字")
				return
			}
		}
		var row DocumentTemplate
		err := db.Transaction(func(tx *gorm.DB) error {
			var latest int
			if err := tx.Model(&DocumentTemplate{}).Where("kind = ?", req.Kind).Select("COALESCE(MAX(version),0)").Scan(&latest).Error; err != nil {
				return err
			}
			row = DocumentTemplate{Kind: req.Kind, Version: latest + 1, Title: strings.TrimSpace(req.Title), Clauses: req.Clauses, CreatedBy: currentUserID(c)}
			return tx.Create(&row).Error
		})
		if err != nil {
			response.ErrorInternal(c, "模板版本保存失败，请刷新重试")
			return
		}
		response.Success(c, row)
	}
}

func buildIssuedDocument(ctx context.Context, kind, key string, ledgerID, userID uint, snapshot documentSnapshot) (*IssuedDocument, error) {
	snapshot.Schema = 1
	snapshot.Number = strings.ToUpper(kind[:1]) + "-" + time.Now().Format("20060102") + "-" + strings.ToUpper(uuid.NewString()[:8])
	tpl, err := template.New("document").Parse(issuedDocumentHTML)
	if err != nil {
		return nil, err
	}
	var html bytes.Buffer
	if err = tpl.Execute(&html, snapshot); err != nil {
		return nil, err
	}
	pdf, err := documentPDFRenderer(ctx, html.String())
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(pdf)
	return &IssuedDocument{LedgerID: ledgerID, CreatedBy: userID, Kind: kind, RequestKey: key, Number: snapshot.Number, TemplateVersion: snapshot.TemplateVersion,
		Snapshot: string(raw), PDF: pdf, SHA256: hex.EncodeToString(hash[:]), FileSize: len(pdf)}, nil
}

func documentQuery(db *gorm.DB) *gorm.DB { return db.Omit("PDF", "Snapshot") }

var errRoomHasIssuedDocuments = errors.New("该房源已有归档合同或收据，不能删除或跨台账移动，请保留原台账记录")

func protectRoomDocuments(db *gorm.DB, roomID uint) error {
	if err := automationRoomGuard(db, roomID); err != nil {
		return err
	}
	var count int64
	err := db.Model(&IssuedDocument{}).Where(
		"tenant_id IN (?) OR bill_id IN (?)",
		db.Model(&Tenant{}).Select("id").Where("room_id = ?", roomID),
		db.Model(&Bill{}).Select("id").Where("room_id = ?", roomID),
	).Count(&count).Error
	if err != nil {
		return err
	}
	if count > 0 {
		return errRoomHasIssuedDocuments
	}
	return nil
}

func handleAgreementList(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		t, ok := findUserTenant(db, c)
		if !ok {
			return
		}
		var rows []IssuedDocument
		if documentQuery(db).Where("org_id = ? AND tenant_id = ? AND kind IN ?", t.LedgerID, t.ID, []string{"lease", "renewal"}).Order("id DESC").Find(&rows).Error != nil {
			response.ErrorInternal(c, "合同版本加载失败")
			return
		}
		response.Success(c, rows)
	}
}

func handlePortalAgreements(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		t, ok := portalTenantRequired(db, c)
		if !ok {
			return
		}
		var rows []IssuedDocument
		if documentQuery(db).Where("org_id = ? AND tenant_id = ? AND kind IN ?", t.LedgerID, t.ID, []string{"lease", "renewal"}).Order("id DESC").Find(&rows).Error != nil {
			response.ErrorInternal(c, "合同版本加载失败")
			return
		}
		response.Success(c, rows)
	}
}

func handleIssuedDocumentFile(db *gorm.DB, portal bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var doc IssuedDocument
		q := db.Where("id = ?", c.Param("id"))
		if portal {
			t, ok := portalTenantRequired(db, c)
			if !ok {
				return
			}
			visible := portalBillQuery(db, t).Select("id")
			q = q.Where("org_id = ? AND ((tenant_id = ? AND kind IN ?) OR (bill_id IN (?) AND kind IN ?))", t.LedgerID, t.ID, []string{"lease", "renewal"}, visible, []string{"receipt", "reversal"})
		}
		if q.First(&doc).Error != nil {
			response.ErrorNotFound(c, "凭证不存在")
			return
		}
		if !portal {
			module := ModuleTenants
			if doc.Kind == "receipt" || doc.Kind == "reversal" {
				module = ModuleBills
			}
			if !inLedgerScope(c, doc.LedgerID) || permRank[ledgerPerms(db, currentUserID(c), doc.LedgerID)[module]] < permRank[PermView] {
				response.ErrorNotFound(c, "凭证不存在")
				return
			}
		}
		hash := sha256.Sum256(doc.PDF)
		if hex.EncodeToString(hash[:]) != doc.SHA256 {
			response.ErrorInternal(c, "归档文件校验失败，请联系管理员恢复备份")
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Disposition", contractDisposition(c, doc.Number+".pdf", c.Query("download") == "1"))
		c.Data(http.StatusOK, "application/pdf", doc.PDF)
	}
}

func handleAgreementIssue(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		t, ok := findUserTenant(db, c)
		if !ok {
			return
		}
		if !requireLedgerWrite(db, c, t.LedgerID, ModuleTenants, PermEdit) {
			return
		}
		var req struct {
			Kind       string `json:"kind"`
			RenewalID  uint   `json:"renewal_id"`
			TemplateID uint   `json:"template_id"`
			RequestKey string `json:"request_key"`
		}
		if c.ShouldBindJSON(&req) != nil || (req.Kind != "lease" && req.Kind != "renewal") || len(req.RequestKey) < 8 || len(req.RequestKey) > 100 {
			response.ErrorBadRequest(c, "请选择合同类型并提供有效请求编号")
			return
		}
		var existing IssuedDocument
		if documentQuery(db).Where("org_id = ? AND request_key = ?", t.LedgerID, req.RequestKey).First(&existing).Error == nil {
			if existing.TenantID != t.ID || existing.Kind != req.Kind || existing.RenewalID != req.RenewalID || (req.TemplateID > 0 && existing.TemplateID != req.TemplateID) {
				response.ErrorBadRequest(c, "请求编号已用于其他合同")
				return
			}
			response.Success(c, existing)
			return
		}
		if err := seedDocumentTemplates(db); err != nil {
			response.ErrorInternal(c, "合同模板不可用")
			return
		}
		var templateVersion DocumentTemplate
		q := db.Where("kind = ?", req.Kind)
		if req.TemplateID > 0 {
			q = q.Where("id = ?", req.TemplateID)
		}
		if q.Order("version DESC").First(&templateVersion).Error != nil {
			response.ErrorBadRequest(c, "合同模板不存在")
			return
		}
		var room Room
		if db.Where("id = ? AND org_id = ?", t.RoomID, t.LedgerID).First(&room).Error != nil {
			response.ErrorNotFound(c, "房源不存在")
			return
		}
		start, end, rent := t.MoveInDate, t.LeaseEndDate, room.DefaultRent
		if req.Kind == "renewal" {
			r, err := leaseRenewalByID(db, t.LedgerID, req.RenewalID)
			if err != nil || r.TenantID != t.ID || r.RoomID != room.ID || r.EffectiveFrom == "" {
				response.ErrorBadRequest(c, "请选择有明确生效日的本租客续租记录")
				return
			}
			start, end, rent = r.EffectiveFrom, r.ToEndDate, r.RentAfter
		} else {
			var first LeaseRenewal
			if db.Where("org_id = ? AND tenant_id = ? AND room_id = ? AND effective_from <> ''", t.LedgerID, t.ID, room.ID).Order("effective_from ASC").First(&first).Error == nil {
				end, rent = first.FromEndDate, first.RentBefore
			}
		}
		from, fromOK := parseLeaseDate(start)
		to, toOK := parseLeaseDate(end)
		if !fromOK || !toOK || to.Before(from) || strings.TrimSpace(room.LandlordName) == "" {
			response.ErrorBadRequest(c, "请先补齐有效入住日期、合同到期日和房源出租方姓名")
			return
		}
		cfg := effectiveTenantsBilling(db, &room, []Tenant{*t})
		payDay := "双方约定日期"
		if cfg.PayDay > 0 {
			payDay = fmt.Sprintf("每个缴费周期首月 %d 日前", cfg.PayDay)
		}
		_, elec, gas := defaultPrices(db)
		if room.ElecPrice > 0 {
			elec = room.ElecPrice
		}
		if room.GasPrice > 0 {
			gas = room.GasPrice
		}

		snapshot := documentSnapshot{Title: templateVersion.Title, TemplateVersion: templateVersion.Version, Clauses: templateVersion.Clauses, Signatures: true,
			Footer: "本协议一式两份，甲乙双方各执一份，自双方签字之日起生效。此 PDF 为待签署打印稿，签署后可上传扫描件存档。",
			Lines:  []documentLine{{"出租方（甲方）", room.LandlordName + "  联系电话：" + room.LandlordPhone}, {"承租方（乙方）", t.Name + "  联系电话：" + t.Phone}, {"乙方证件号", t.IDCard}, {"乙方证件住址", t.Address}, {"房屋坐落", room.Address + " " + roomLocationLabel(room)}, {"建筑面积", fmt.Sprintf("%.2f 平方米（未填为 0）", room.AreaSize)}, {"租期", start + " 至 " + end}, {"月租金", contractFeeValue(db, &room, t, feeKeyRent, formatAmount(rent)+" 元；月中按当月实际天数折算")}, {"缴费约定", cycleLabel(cfg.PayCycle) + "；" + payDay}, {"押金", formatAmount(t.Deposit) + " 元"}, {"水费", contractFeeValue(db, &room, t, feeKeyWater, waterModeLabel(cfg.WaterMode)+" "+formatAmount(cfg.WaterAmount)+map[string]string{waterModeMeter: " 元/吨", waterModeMonthly: " 元/月"}[cfg.WaterMode])}, {"电费", contractFeeValue(db, &room, t, feeKeyElec, formatAmount(elec)+" 元/度")}, {"燃气费", contractFeeValue(db, &room, t, feeKeyGas, formatAmount(gas)+" 元/方")}, {"卫生费", contractFeeValue(db, &room, t, feeKeySanitation, formatAmount(room.DefaultSanitationFee)+" 元/月，随租金周期收取")}, {"管理费", contractFeeValue(db, &room, t, feeKeyManagement, formatAmount(room.DefaultManagementFee)+" 元/月，随租金周期收取")}}}
		snapshot.Lines = append(snapshot.Lines, automationAgreementLine(db, t.LedgerID, room.ID, []uint{t.ID})...)
		for _, fee := range enabledCustomFeeItems(db, room.LedgerID) {
			value := formatAmount(fee.DefaultAmount) + " 元/月，" + cycleLabel(fee.Cycle)
			if season := seasonRangeText(fee); season != "" {
				value += "；仅 " + season + " 收取"
			}
			snapshot.Lines = append(snapshot.Lines, documentLine{fee.Name, contractFeeValue(db, &room, t, fee.Key, value)})
		}
		doc, err := buildIssuedDocument(c.Request.Context(), req.Kind, req.RequestKey, t.LedgerID, currentUserID(c), snapshot)
		if err != nil {
			response.ErrorInternal(c, err.Error())
			return
		}
		doc.TenantID, doc.TemplateID, doc.RenewalID = t.ID, templateVersion.ID, req.RenewalID
		if db.Transaction(func(tx *gorm.DB) error {
			var parents int64
			if err := tx.Model(&Tenant{}).Where("id = ? AND room_id = ? AND org_id = ?", t.ID, room.ID, t.LedgerID).Count(&parents).Error; err != nil {
				return err
			}
			if parents != 1 {
				return errors.New("租客归属已变化")
			}
			if err := tx.Model(&Room{}).Where("id = ? AND org_id = ?", room.ID, t.LedgerID).Count(&parents).Error; err != nil {
				return err
			}
			if parents != 1 {
				return errors.New("房源归属已变化")
			}
			return tx.Create(doc).Error
		}) != nil {
			response.ErrorBadRequest(c, "合同归档失败，请使用原请求编号重试")
			return
		}
		doc.PDF = nil
		response.Success(c, doc)
	}
}

// Capture exclusions and ledger switches alongside the price; the printed
// agreement must not promise charges that the bill engine has disabled.
func contractFeeValue(db *gorm.DB, room *Room, tenant *Tenant, key, value string) string {
	for _, excluded := range append(append([]string{}, tenant.ExcludedFees...), disabledBuiltInFeeKeys(db, room.LedgerID)...) {
		if excluded == key {
			return "不向乙方收取"
		}
	}
	return value
}

const issuedDocumentHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><style>
@page{size:A4;margin:16mm}*{box-sizing:border-box}body{font-family:"Microsoft YaHei","Noto Sans CJK SC",sans-serif;color:#15212b;font-size:12px;line-height:1.65;margin:0}h1{text-align:center;font-size:23px;margin:0 0 9px}header{text-align:center;color:#52616d;font-size:10px;margin-bottom:18px}table{width:100%;border-collapse:collapse;margin-bottom:14px}th,td{border:1px solid #bac3cb;padding:5px 9px;text-align:left;overflow-wrap:anywhere}th{width:24%;font-weight:500;background:#f5f7f8}tr{break-inside:avoid}ol{padding-left:22px;margin:0}li{padding-left:2px;margin-bottom:7px;break-inside:avoid;white-space:pre-wrap;overflow-wrap:anywhere}.end{break-inside:avoid}.sign{display:flex;justify-content:space-between;margin-top:18px;break-inside:avoid}.sign div{width:45%;height:70px}footer{margin-top:8px;font-size:10px;color:#52616d;break-inside:avoid}</style></head><body>
<h1>{{.Title}}</h1><header>编号 {{.Number}}{{if .TemplateVersion}} · 模板版本 {{.TemplateVersion}}{{end}}</header><table>{{range .Lines}}<tr><th>{{.Label}}</th><td>{{.Value}}</td></tr>{{end}}</table>
{{if .Clauses}}<ol>{{range .Clauses}}<li>{{.}}</li>{{end}}</ol>{{end}}<section class="end">{{if .Signatures}}<section class="sign"><div>甲方（签字／盖章）：<br><br>日期：　　　年　　月　　日</div><div>乙方（签字）：<br><br>日期：　　　年　　月　　日</div></section>{{end}}<footer>{{.Footer}}</footer></section></body></html>`
