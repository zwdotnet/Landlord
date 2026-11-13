package rental

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"smallgo/server/response"
	"smallgo/server/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// A minimal room picker for meter readers, without rent, tenant or contact data.
type meterRoom struct {
	ID        uint   `json:"id"`
	RoomNo    string `json:"room_no"`
	Community string `json:"community"`
	Building  string `json:"building"`
}

func handleMeterRooms(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		page, size := utils.NormalizePage(utils.Atoi(c.Query("page"), 1), utils.Atoi(c.Query("pageSize"), 10))
		q, empty := scopeLedger(db.Model(&Room{}), c)
		if empty {
			response.SuccessPage(c, []meterRoom{}, 0, page, size)
			return
		}
		if keyword := strings.TrimSpace(c.Query("keyword")); keyword != "" {
			q = q.Where("(room_no LIKE ? OR community LIKE ? OR building LIKE ?)", "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
		}
		var total int64
		var rooms = make([]meterRoom, 0)
		if q.Count(&total).Error != nil || q.Select("id, room_no, community, building").Order("id ASC").Offset(utils.Offset(page, size)).Limit(size).Find(&rooms).Error != nil {
			response.ErrorInternal(c, "读取抄表房源失败，请重试")
			return
		}
		response.SuccessPage(c, rooms, total, page, size)
	}
}

func handleMeterContext(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		id, err := strconv.ParseUint(c.Query("room_id"), 10, 64)
		period := strings.TrimSpace(c.Query("period"))
		if err != nil || id == 0 || !isValidPeriod(period) {
			response.ErrorBadRequest(c, "请选择房源与正确账期")
			return
		}
		var result gin.H
		denied := false
		err = db.Transaction(func(tx *gorm.DB) error {
			room, ok := loadRoom(tx, c, uint(id))
			if !ok {
				denied = true
				return errors.New("room unavailable")
			}
			previous := gin.H{"period": "", "is_initial": true, "water": room.InitialWater, "elec": room.InitialElec, "gas": room.InitialGas}
			var prev MeterRecord
			q := tx.Where("org_id = ? AND room_id = ? AND period < ?", room.LedgerID, room.ID, period).Order("period DESC, id DESC").First(&prev)
			if q.Error == nil {
				previous = gin.H{"period": prev.Period, "is_initial": false, "water": prev.Water, "elec": prev.Elec, "gas": prev.Gas}
			} else if !errors.Is(q.Error, gorm.ErrRecordNotFound) {
				return q.Error
			}
			var current MeterRecord
			var selected interface{}
			q = tx.Where("org_id = ? AND room_id = ? AND period = ?", room.LedgerID, room.ID, period).First(&current)
			if q.Error == nil {
				selected = current
			} else if !errors.Is(q.Error, gorm.ErrRecordNotFound) {
				return q.Error
			}
			var bills int64
			if err := tx.Model(&Bill{}).Where("org_id = ? AND room_id = ? AND period = ?", room.LedgerID, room.ID, period).Count(&bills).Error; err != nil {
				return err
			}
			result = gin.H{"room": gin.H{"id": room.ID, "room_no": room.RoomNo}, "period": period, "previous": previous, "current": selected, "bill_exists": bills > 0}
			return nil
		})
		if denied {
			return
		}
		if err != nil {
			response.ErrorInternal(c, "读取抄表读数失败，请重试")
			return
		}
		response.Success(c, result)
	}
}

type meterExpectation struct {
	ID        uint      `json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
}

type meterRequest struct {
	RoomID   uint              `json:"room_id" binding:"required"`
	Period   string            `json:"period" binding:"required"`
	Water    float64           `json:"water"`
	Elec     float64           `json:"elec"`
	Gas      float64           `json:"gas"`
	Note     string            `json:"note"`
	Expected *meterExpectation `json:"expected"`
}

func (req meterRequest) valid() bool {
	for _, value := range []float64{req.Water, req.Elec, req.Gas} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return utf8.RuneCountInString(req.Note) <= 500 && (req.Expected == nil || req.Expected.ID == 0 || !req.Expected.UpdatedAt.IsZero())
}

var errMeterChanged = errors.New("抄表记录已被更新，请核对服务器记录后重新载入")
var errMeterRoomGone = errors.New("房源不存在")

// No schema changes: reserve SQLite's write lock before looking up (room, month).
// Optional optimistic concurrency keeps existing Web callers compatible. An
// identical retry returns the same record without a second write or new version.
func saveMeterRecord(tx *gorm.DB, room *Room, userID uint, req meterRequest) (MeterRecord, bool, error) {
	lock := tx.Model(&Room{}).Where("id = ? AND org_id = ?", room.ID, room.LedgerID).UpdateColumn("id", gorm.Expr("id"))
	if lock.Error != nil {
		return MeterRecord{}, false, lock.Error
	}
	if lock.RowsAffected != 1 {
		return MeterRecord{}, false, errMeterRoomGone
	}
	var rec MeterRecord
	err := tx.Where("org_id = ? AND room_id = ? AND period = ?", room.LedgerID, room.ID, req.Period).First(&rec).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return rec, false, err
	}
	exists := err == nil
	same := exists && rec.Water == req.Water && rec.Elec == req.Elec && rec.Gas == req.Gas && rec.Note == req.Note
	if req.Expected != nil {
		matches := (!exists && req.Expected.ID == 0) || (exists && req.Expected.ID == rec.ID && req.Expected.UpdatedAt.Equal(rec.UpdatedAt))
		if !matches && !same {
			return rec, false, errMeterChanged
		}
	}
	if same {
		return rec, false, nil
	}
	if !exists {
		rec = MeterRecord{UserID: userID, LedgerID: room.LedgerID, RoomID: room.ID, Period: req.Period}
	}
	rec.Water, rec.Elec, rec.Gas, rec.Note = req.Water, req.Elec, req.Gas, req.Note
	if exists {
		// Explicitly advance the existing version even with a coarse clock.
		next := time.Now()
		if !next.After(rec.UpdatedAt) {
			next = rec.UpdatedAt.Add(time.Nanosecond)
		}
		err = tx.Model(&rec).Updates(map[string]interface{}{"water": rec.Water, "elec": rec.Elec, "gas": rec.Gas, "note": rec.Note, "updated_at": next}).Error
		rec.UpdatedAt = next
	} else {
		err = tx.Create(&rec).Error
	}
	if err != nil {
		return rec, false, err
	}
	return rec, true, nil
}

func meterSaveError(c *gin.Context, err error) {
	if errors.Is(err, errMeterChanged) {
		response.Error(c, http.StatusConflict, http.StatusConflict, err.Error())
	} else if errors.Is(err, errMeterRoomGone) {
		response.ErrorNotFound(c, err.Error())
	} else {
		response.ErrorInternal(c, "保存抄表记录失败，请核对后重试")
	}
}
