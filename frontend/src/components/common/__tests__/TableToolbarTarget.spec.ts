import { afterEach, describe, expect, it } from 'vitest'
import { defineComponent, h, inject, nextTick, ref, Teleport, type Ref } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import TableToolbarTarget from '../TableToolbarTarget.vue'

let wrapper: VueWrapper
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })

describe('inline table toolbar placement', () => {
  it('keeps sorting inside the filter flow and falls back safely when the inline target disappears', async () => {
    const showInline = ref(true)
    const Sorter = defineComponent({
      setup() {
        const target = inject<Ref<HTMLElement | null>>('tableToolbarTarget')!
        const actionsTarget = inject<Ref<HTMLElement | null>>('tableActionsTarget')!
        return () => [
          h(Teleport, { to: target.value || 'body', disabled: !target.value }, [
            h('button', { 'data-test': 'sort' }, 'Sort')
          ]),
          h(Teleport, { to: actionsTarget.value || 'body', disabled: !actionsTarget.value }, [
            h('button', { 'data-test': 'columns' }, 'Columns')
          ])
        ]
      }
    })
    wrapper = mount(defineComponent({
      setup: () => () => h(TablePageLayout, {}, {
        actions: () => h('header', {}, [h(TableToolbarTarget, { kind: 'actions' })]),
        filters: () => h('div', { class: 'filter-controls' }, [
          h('input', { 'aria-label': 'Search' }),
          showInline.value ? h(TableToolbarTarget) : null
        ]),
        table: () => h(Sorter)
      })
    }), { attachTo: document.body })
    await nextTick()
    await nextTick()
    expect(wrapper.find('.filter-controls .inline-table-tools button').exists()).toBe(true)
    expect(wrapper.find('.table-filter-tools button').exists()).toBe(false)
    expect(wrapper.find('header .table-header-tools [data-test="columns"]').exists()).toBe(true)
    showInline.value = false
    await nextTick()
    await nextTick()
    expect(wrapper.find('.table-filter-tools button').exists()).toBe(true)
    expect(wrapper.find('header .table-header-tools [data-test="columns"]').exists()).toBe(true)
  })
})
