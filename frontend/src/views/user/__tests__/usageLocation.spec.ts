import {describe,expect,it} from 'vitest'
import {parseUsageLocation,usageLocationQuery} from '../usageLocation'
const defaults = {start:'2026-10-17T00:00:00.000Z',end:'2026-10-18T00:00:00.000Z',pageSize:20}
describe('usage route schema',()=>{
  it('preserves key/model/date/pagination and uses canonical key instead of its legacy alias',()=>{
    const state = parseUsageLocation({view:'records',api_key_id:'9',key:'2',model:'gpt-6',start_date:'2026-10-10',end_date:'2026-10-12',page:'3',page_size:'50',sort_by:'model',sort_order:'asc'},defaults)
    expect(state.filters).toMatchObject({api_key_id:9,model:'gpt-6'})
    expect(usageLocationQuery(state)).toMatchObject({start_date:'2026-10-10',end_date:'2026-10-12',page:3,page_size:50,api_key_id:9})
    expect(usageLocationQuery(state)).not.toHaveProperty('key')
  })
  it('opens failed deep links against the error data source without making completed a forced error filter',()=>{
    expect(parseUsageLocation({view:'errors'},defaults).tab).toBe('errors')
    expect(parseUsageLocation({view:'records',tab:'errors',status_code:'429',category:'rate_limit'},defaults)).toMatchObject({tab:'errors',statusCode:429,category:'rate_limit'})
    expect(parseUsageLocation({status:'completed'},defaults).tab).toBe('usage')
  })
  it('rejects invalid ranges, arrays, privileged IDs and unknown query context',()=>{
    const state=parseUsageLocation({api_key_id:['9','2'],key:'-2',start_date:'2026-02-31',end_date:'2026-03-01',page:'0',page_size:'100000',user_id:'9',account_id:'12',payment_id:'a',request_type:'evil',category:'secret',sort_by:'user_id'},defaults)
    expect(state).toMatchObject({start:defaults.start,end:defaults.end,page:1,pageSize:20,sortBy:'created_at',category:''})
    expect(state.filters.api_key_id).toBeUndefined()
    expect(usageLocationQuery(state)).not.toHaveProperty('user_id')
    expect(usageLocationQuery(state)).not.toHaveProperty('payment_id')
  })
  it('uses the legacy key alias and retains explicit zero balance billing and false compaction',()=>{
    const state=parseUsageLocation({key:'18',billing_type:'0',native_compaction_v2:'false'},defaults)
    expect(usageLocationQuery(state)).toMatchObject({api_key_id:18,billing_type:'0',native_compaction_v2:'false'})
  })
})
