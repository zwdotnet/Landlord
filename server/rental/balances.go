package rental

import "fmt"

// Net only within the same ledger, room and explicit recipient set. Legacy
// unassigned bills stand alone; a display name never identifies a tenancy.
// Across periods use current charges, excluding carry-in already billed before.
func outstandingByTenancy(bills []Bill, usePayable bool) float64 {
	balances := make(map[string]float64)
	for _, b := range bills {
		key := fmt.Sprintf("bill:%d", b.ID)
		if len(b.RecipientTenantIDs) > 0 {
			key = fmt.Sprintf("%d:%d:%s", b.LedgerID, b.RoomID, tenantKey(b.RecipientTenantIDs))
		}
		amount := b.TotalAmount
		if usePayable {
			amount = b.Payable
		}
		balances[key] += amount - b.PaidAmount
	}
	var outstanding float64
	for _, balance := range balances {
		if balance = round2(balance); balance > 0 {
			outstanding += balance
		}
	}
	return round2(outstanding)
}
