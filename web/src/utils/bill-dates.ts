import { cycleMonths } from './cycle'

export interface RentBasisBill {
  period: string
  pay_cycle?: string
  rent?: number
  rent_start_date?: string
  rent_end_date?: string
  rent_calculation?: string
  rent_basis_amount?: number
  rent_basis_source?: string
}

export function calendarRange(period: string, months = 1) {
  if (!/^\d{4}-(0[1-9]|1[0-2])$/.test(period)) return { start: '', end: '' }
  const [year, month] = period.split('-').map(Number)
  const last = new Date(year, month - 1 + months, 0)
  return {
    start: `${period}-01`,
    end: `${last.getFullYear()}-${String(last.getMonth()+1).padStart(2,'0')}-${String(last.getDate()).padStart(2,'0')}`,
  }
}

export function rentRange(bill: RentBasisBill) {
  return bill.rent_start_date && bill.rent_end_date
    ? { start: bill.rent_start_date, end: bill.rent_end_date }
    : calendarRange(bill.period, cycleMonths(bill.pay_cycle))
}

// Old saved item descriptions use months. Expand only complete date ranges,
// leaving arbitrary notes and amounts intact.
export function detailDateLines(detail: string) {
  return detail.replace(/(\d{4}-\d{2})(?!-\d{2})\s*~\s*(\d{4}-\d{2})(?!-\d{2})/g,
    (_, start: string, end: string) => {
      const a = calendarRange(start), b = calendarRange(end)
      return a.start && b.end ? `起 ${a.start}\n止 ${b.end}` : `${start} ~ ${end}`
    }).split('\n')
}
