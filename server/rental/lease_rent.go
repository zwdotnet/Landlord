package rental

import (
	"time"

	"gorm.io/gorm"
)

// Rental's tenancy row and renewal history are retained as the first-stage
// lease identity and dated rent schedule. Issued bills remain snapshots.
func rentForTenantAt(db *gorm.DB, room *Room, tenant *Tenant, date string) float64 {
	var term LeaseRenewal
	q := db.Where("org_id = ? AND room_id = ? AND tenant_id = ? AND effective_from <> ''",
		room.LedgerID, room.ID, tenant.ID)
	if err := q.Session(&gorm.Session{}).Where("effective_from <= ?", date).
		Order("effective_from DESC, id DESC").First(&term).Error; err == nil {
		return term.RentAfter
	}
	// Before the first renewal, use the captured original rate, so preparing a
	// future renewal cannot reprice unissued bills from the existing term.
	if err := q.Order("effective_from ASC, id ASC").First(&term).Error; err == nil {
		return term.RentBefore
	}
	return room.DefaultRent
}

// Rent is accumulated using each calendar month's actual day count and rounded
// once at the end. Joint tenants share one room rent; conflicting rates require
// landlord review instead of adding each person's rent together.
func scheduledBillRent(db *gorm.DB, room *Room, tenants []Tenant, period string, months float64) (float64, bool) {
	if len(tenants) == 0 || months == 0 {
		return round2(room.DefaultRent * months), false
	}
	start, err := time.Parse("2006-01", period)
	if err != nil {
		return 0, true
	}
	schedules := make(map[uint][]LeaseRenewal)
	review := false
	for _, tenant := range tenants {
		var terms []LeaseRenewal
		if err := db.Where("org_id = ? AND room_id = ? AND tenant_id = ?", room.LedgerID, room.ID, tenant.ID).
			Order("effective_from ASC, id ASC").Find(&terms).Error; err != nil {
			return 0, true
		}
		for _, term := range terms {
			if term.EffectiveFrom == "" && term.RentBefore != term.RentAfter {
				review = true
			}
		}
		schedules[tenant.ID] = terms
	}
	var total float64
	for month := 0; month < int(months); month++ {
		first := start.AddDate(0, month, 0)
		last := first.AddDate(0, 1, -1)
		for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
			date := day.Format("2006-01-02")
			found, rate := false, 0.0
			for _, tenant := range tenants {
				if from, ok := parseLeaseDate(tenant.MoveInDate); ok && date < from.Format("2006-01-02") {
					continue
				}
				if to, ok := parseLeaseDate(tenant.MoveOutDate); ok && date > to.Format("2006-01-02") {
					continue
				}
				if to, ok := parseLeaseDate(tenant.LeaseEndDate); ok && date > to.Format("2006-01-02") {
					continue
				}
				current := room.DefaultRent
				initialized, gap := false, false
				for _, term := range schedules[tenant.ID] {
					if term.EffectiveFrom == "" {
						continue
					}
					if !initialized {
						current = term.RentBefore
						initialized = true
					}
					if date >= term.EffectiveFrom {
						current = term.RentAfter
					}
					if end, ok := parseLeaseDate(term.FromEndDate); ok && date > end.Format("2006-01-02") && date < term.EffectiveFrom {
						gap = true
					}
				}
				if gap {
					continue
				}
				if found && current != rate {
					review = true
				}
				if !found {
					found, rate = true, current
				}
			}
			total += rate / float64(last.Day())
		}
	}
	return round2(total), review
}
