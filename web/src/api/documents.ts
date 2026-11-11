import request from './request'
import { currentLedgerQuery } from '../stores/ledger'

export interface IssuedDocument {
  id: number; number: string; kind: 'lease' | 'renewal' | 'receipt' | 'reversal'
  tenant_id: number; renewal_id: number; template_version: number; file_size: number
  sha256: string; created_at: string
}
export interface DocumentTemplate {
  id: number; kind: 'lease' | 'renewal'; version: number; title: string; clauses: string[]
}
export interface ArchivedPayment {
  id: number; bill_id?: number; paid_at: string; amount: number; note: string
  receipt_id?: number; reversal_of_id?: number | null; voided_at?: string | null; void_reason?: string
  items?: { key: string; name: string; amount: number }[]
}
function scope(url: string) {
  const id = currentLedgerQuery()
  return id === null ? url : `${url}?ledger_id=${id}`
}
export const getDocumentTemplates = (admin = false) => request.get(admin ? '/api/rental/platform/document-templates' : scope('/api/rental/document-templates'))
export const publishDocumentTemplate = (data: Pick<DocumentTemplate, 'kind' | 'title' | 'clauses'>) => request.post('/api/rental/platform/document-templates', data)
export const getAgreements = (tenantId: number) => request.get(scope(`/api/rental/tenants/${tenantId}/agreements`))
export const getPortalAgreements = () => request.get('/api/rental/portal/agreements')
export const issueAgreement = (tenantId: number, data: { kind: string; template_id: number; renewal_id: number; request_key: string }) =>
  request.post(scope(`/api/rental/tenants/${tenantId}/agreements`), data, { timeout: 60000 })
export const fetchIssuedPDF = async (id: number, portal = false): Promise<Blob> => {
  const url = portal ? `/api/rental/portal/documents/${id}/file` : scope(`/api/rental/documents/${id}/file`)
  const res = await request.get(url, { responseType: 'blob' })
  return res.data
}
export const reversePayment = (id: number, reason: string, requestKey: string) =>
  request.post(scope(`/api/rental/payments/${id}/reverse`), { reason, request_key: requestKey }, { timeout: 60000 })
