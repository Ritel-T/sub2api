import {describe,expect,it,vi} from 'vitest'
import {flushPromises,mount} from '@vue/test-utils'
import UserUsageRequestDetail from '../UserUsageRequestDetail.vue'
import UserErrorDetailModal from '../UserErrorDetailModal.vue'
const {getDetail}=vi.hoisted(()=>({getDetail:vi.fn()}))
vi.mock('@/api/usage',()=>({getMyErrorDetail:getDetail}))
vi.mock('@/utils/format',()=>({formatDateTime:(v:string)=>v}))
vi.mock('vue-i18n',()=>({useI18n:()=>({t:(k:string)=>k})}))
const global={stubs:{BaseDialog:{template:'<div><slot /></div>'},RouterLink:{name:'RouterLink',props:['to'],template:'<a><slot /></a>'}}}
describe('request support context',()=>{
  it('passes only the successful request ID without guessing an administrator-defined ticket category',()=>{
    const wrapper=mount(UserUsageRequestDetail,{props:{record:{id:1,request_id:'req-123',actual_cost:0.2} as never,supportEnabled:true},global})
    expect(wrapper.findComponent({name:'RouterLink'}).props('to')).toEqual({path:'/support-tickets',query:{request_id:'req-123'}})
    wrapper.unmount()
  })
  it('passes only the error ID without response bodies or a guessed category',async()=>{
    getDetail.mockResolvedValue({id:7,category:'upstream',status_code:500,error_body:'sensitive-response'})
    const wrapper=mount(UserErrorDetailModal,{props:{show:false,errorId:7,supportEnabled:true},global})
    await wrapper.setProps({show:true});await flushPromises()
    expect(wrapper.findComponent({name:'RouterLink'}).props('to')).toEqual({path:'/support-tickets',query:{error_id:'7'}})
    wrapper.unmount()
  })
  it('does not expose the action when the caller has no personal-ticket permission',()=>{
    const wrapper=mount(UserUsageRequestDetail,{props:{record:{request_id:'req-123',actual_cost:0.2} as never,supportEnabled:false},global})
    expect(wrapper.findComponent({name:'RouterLink'}).exists()).toBe(false)
    wrapper.unmount()
  })
})
