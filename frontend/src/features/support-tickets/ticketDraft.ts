import type { CreateSupportTicketRequest } from '@/api/supportTickets'

export function supportTicketDraft(query: Record<string, unknown>): Partial<CreateSupportTicketRequest> | null {
  const identifier = (value: unknown) => typeof value === 'string' && /^[a-zA-Z0-9_.:-]{1,160}$/.test(value) ? value : ''
  const request = identifier(query.request_id)
  const order = identifier(query.order_id)
  const error = identifier(query.error_id)
  const references = [request && `request_id: ${request}`, order && `order_id: ${order}`, error && `error_id: ${error}`].filter(Boolean)
  if (!references.length) return null
  // References belong to the user's editable message; never infer categories from arbitrary URL text.
  return { body: references.join('\n') + '\n\n' }
}
