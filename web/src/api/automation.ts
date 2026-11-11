import request from './request'
import { currentLedgerQuery } from '../stores/ledger'
function withLedger(params: Record<string, unknown>) { const id=currentLedgerQuery(); return id===null ? params : { ...params, ledger_id:id } }
function withLedgerUrl(url: string) { const id=currentLedgerQuery(); return id===null ? url : `${url}?ledger_id=${id}` }
export interface AutomationPlan { id: number; room_id: number; room_no: string; tenant_ids: number[]; enabled: boolean; meter_day: number; bill_day: number; due_day: number; lead_days: number; start_period: string; version: number }
export interface CycleTask { id: number; plan_id: number; period: string; room_no: string; meter_date: string; bill_date: string; due_date: string; status: string; detail: string; bill_id: number; attempts: number }
export interface InboxNotice { id: number; kind: string; title: string; body: string; url: string; read_at?: string; created_at: string }
export const getAutomationPlans = () => request.get('/api/rental/automation/plans', { params: withLedger({}) })
export const getCycleTasks = () => request.get('/api/rental/automation/tasks', { params: withLedger({}) })
export const saveAutomationPlan = (data: Partial<AutomationPlan>) => request.post(withLedgerUrl('/api/rental/automation/plans'), data)
export const checkAutomation = () => request.post(withLedgerUrl('/api/rental/automation/run'))
export const getInbox = () => request.get('/api/rental/notifications')
export const readNotice = (id: number) => request.post(`/api/rental/notifications/${id}/read`)
export const getPushConfig = () => request.get('/api/rental/push/config')
export const saveSubscription = (data: PushSubscriptionJSON) => request.post('/api/rental/push/subscriptions', data)
export const removeSubscription = (endpoint: string) => request.delete('/api/rental/push/subscriptions', { data: { endpoint } })
export const testPush = () => request.post('/api/rental/push/test')
export const getPushStatus = () => request.get('/api/rental/push/status')
