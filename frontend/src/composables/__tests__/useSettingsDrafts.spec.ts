import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'
import { useSettingsDrafts } from '../useSettingsDrafts'

describe('partitioned settings drafts', () => {
  it('keeps unrelated drafts dirty after saving and normalizing one partition', () => {
    const general = reactive({ name: 'Saved' }), security = reactive({ enabled: false })
    const drafts = useSettingsDrafts()
    drafts.register('general', { read: () => general, write: value => Object.assign(general, value) })
    drafts.register('security', { read: () => security, write: value => Object.assign(security, value) })
    drafts.mark('general'); drafts.mark('security')
    expect(drafts.anyDirty.value).toBe(false)
    general.name = '  New  '; security.enabled = true
    const submitted = drafts.capture('general')
    drafts.accept('general', submitted, { name: 'New' })
    expect(general.name).toBe('New')
    expect(drafts.dirty('general')).toBe(false)
    expect(drafts.dirty('security')).toBe(true)
    drafts.discard('security')
    expect(security.enabled).toBe(false)
    expect(drafts.anyDirty.value).toBe(false)
  })
  it('retains a newer draft while acknowledging the older server write', () => {
    const form = reactive({ name: 'old' }), drafts = useSettingsDrafts()
    drafts.register('one', { read: () => form, write: value => Object.assign(form, value) }); drafts.mark('one')
    form.name = 'submitted'; const submitted = drafts.capture('one')
    form.name = 'newer'; drafts.accept('one', submitted, { name: 'submitted' })
    expect(form.name).toBe('newer'); expect(drafts.dirty('one')).toBe(true)
    drafts.discard('one'); expect(form.name).toBe('submitted')
  })
  it('does not enable saving defaults after an unsuccessful load', () => {
    const form = reactive({ secret: '' }), drafts = useSettingsDrafts()
    drafts.register('optional', { read: () => form, write: value => Object.assign(form, value) })
    form.secret = 'draft'; expect(drafts.dirty('optional')).toBe(false)
    drafts.mark('optional'); form.secret = 'edited'; expect(drafts.dirty('optional')).toBe(true)
  })
})
