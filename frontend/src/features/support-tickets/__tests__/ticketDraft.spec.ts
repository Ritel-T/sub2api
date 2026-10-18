import { describe, expect, it } from 'vitest'
import { supportTicketDraft } from '../ticketDraft'

describe('editable support references', () => {
  it('keeps original request, order and diagnostic identities as plain message text', () => {
    expect(supportTicketDraft({ request_id: 'req_abc-123', order_id: '42', error_id: '7' })).toEqual({ body: 'request_id: req_abc-123\norder_id: 42\nerror_id: 7\n\n' })
  })
  it('never imports arbitrary body, HTML, secrets or categories from query', () => {
    expect(supportTicketDraft({ request_id: '<script>', order_id: ['1', '2'], body: 'untrusted', category: 'anything' })).toBeNull()
    expect(supportTicketDraft({ error_id: '7', category: 'api_error' })).toEqual({ body: 'error_id: 7\n\n' })
  })
})
