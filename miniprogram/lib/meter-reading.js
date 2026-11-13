function currentMonth() { return new Date(Date.now() + 8 * 3600000).toISOString().slice(0, 7) }
function validPeriod(value) { return /^\d{4}-(0[1-9]|1[0-2])$/.test(value) && Number(value.slice(0, 4)) > 0 }
function expected(record) { return record ? { id: record.id, updated_at: record.updated_at } : { id: 0 } }
function sameVersion(record, version) { return record ? record.id === version.id && record.updated_at === version.updated_at : version.id === 0 }
function sameReading(record, payload) { return !!record && ['water', 'elec', 'gas'].every(key => Number(record[key]) === payload[key]) && (record.note || '') === payload.note }
function payload(data, version) {
  if (!data.room || !validPeriod(data.period)) throw new Error('请选择房源与正确账期')
  const result = { room_id: data.room.id, period: data.period, expected: version, note: data.note.trim() }
  for (const key of ['water', 'elec', 'gas']) {
    const text = String(data[key]).trim()
    if (!/^(?:\d+(?:\.\d*)?|\.\d+)$/.test(text) || !Number.isFinite(Number(text)) || Number(text) < 0) throw new Error('请完整填写水、电、燃气的非负读数；未使用请明确填写0')
    result[key] = Number(text)
  }
  if (result.note.length > 500) throw new Error('备注最多500字')
  return result
}
module.exports = { currentMonth, validPeriod, expected, sameVersion, sameReading, payload }
