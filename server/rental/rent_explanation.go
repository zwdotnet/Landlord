package rental

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type rentSlice struct {
	start, end      string
	rate            float64
	days, monthDays int
}

type rentBreakdown struct{ slices []rentSlice }

func (r *rentBreakdown) add(date string, rate float64, monthDays int) {
	if n := len(r.slices); n > 0 {
		last := &r.slices[n-1]
		end, _ := time.Parse("2006-01-02", last.end)
		if last.start[:7] == date[:7] && last.rate == rate && end.AddDate(0, 0, 1).Format("2006-01-02") == date {
			last.end = date
			last.days++
			return
		}
	}
	r.slices = append(r.slices, rentSlice{date, date, rate, 1, monthDays})
}

func snapshotRentBasis(db *gorm.DB, b *Bill, room *Room, tenants []Tenant, months float64) {
	var details rentBreakdown
	amount, review := scheduledBillRentWithDetails(db, room, tenants, b.Period, months, &details)
	b.Rent, b.RequiresManualRent = amount, review
	b.RentBasisAmount = amount
	lines := make([]string, 0, len(details.slices)+1)
	for _, s := range details.slices {
		lines = append(lines, fmt.Sprintf("%s ~ %s：%s × %d ÷ %d = %.2f 元", s.start, s.end, fmtAmount(s.rate), s.days, s.monthDays, s.rate*float64(s.days)/float64(s.monthDays)))
	}
	if len(details.slices) > 0 {
		b.RentStartDate = details.slices[0].start
		b.RentEndDate = details.slices[len(details.slices)-1].end
		lines = append(lines, fmt.Sprintf("按各月实际天数累计，最终四舍五入：%.2f 元", amount))
		b.RentCalculation = strings.Join(lines, "\n")
	}
}

// Legacy bills have no frozen rent basis. Provide a clearly marked reference
// from their explicit recipients, without changing historical amounts or rows.
func fillLegacyRentBasis(db *gorm.DB, b *Bill) {
	if b.RentStartDate != "" || b.RentCalculation != "" || b.Rent <= 0 || len(b.RecipientTenantIDs) == 0 {
		return
	}
	var room Room
	var tenants []Tenant
	if db.Where("id = ? AND org_id = ?", b.RoomID, b.LedgerID).First(&room).Error != nil {
		return
	}
	if db.Where("id IN ? AND room_id = ? AND org_id = ?", b.RecipientTenantIDs, b.RoomID, b.LedgerID).Find(&tenants).Error != nil || len(tenants) != len(b.RecipientTenantIDs) {
		return
	}
	var ref Bill
	ref.Period, ref.PayCycle = b.Period, b.PayCycle
	snapshotRentBasis(db, &ref, &room, tenants, float64(cycleMonths(b.PayCycle)))
	if ref.RequiresManualRent {
		return
	}
	b.RentStartDate, b.RentEndDate = ref.RentStartDate, ref.RentEndDate
	b.RentBasisAmount, b.RentCalculation = ref.RentBasisAmount, ref.RentCalculation
	b.RentBasisSource = "current_lease_reference"
}
