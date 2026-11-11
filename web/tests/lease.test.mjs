import assert from 'node:assert/strict'
import { test } from 'node:test'
import ts from 'typescript'
import { readFileSync } from 'node:fs'

// Execute the actual TypeScript date helpers, without duplicating their math.
const source = readFileSync(new URL('../src/utils/lease.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2022 } })
const lease = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)

test('renewal starts the day after an inclusive end date', () => {
  const today = new Date(2026, 10, 11)
  assert.equal(lease.renewalEndDateStr('2027-09-30', 12, today), '2028-09-30')
  assert.equal(lease.formatLeaseDate(lease.renewBaseDate('2026-11-11', today)), '2026-11-12')
})

test('expired renewal starts today; month-end and leap-year dates remain valid', () => {
  assert.equal(lease.renewalEndDateStr('2026-01-01', 3, new Date(2026, 10, 11)), '2027-02-10')
  assert.equal(lease.leaseEndDateStr('2026-01-31', 1), '2026-02-27')
  assert.equal(lease.leaseEndDateStr('2024-01-31', 1), '2024-02-28')
  assert.equal(lease.leaseEndDateStr('2026-02-30', 1), '')
})
