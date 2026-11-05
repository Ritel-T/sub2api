/**
 * NavigationProgress 组件单元测试
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import NavigationProgress from '../../common/NavigationProgress.vue'

// Mock useNavigationLoadingState
const mockIsLoading = ref(false)

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    isLoading: mockIsLoading
  })
}))

describe('NavigationProgress', () => {
  beforeEach(() => {
    mockIsLoading.value = false
  })

  it('应该有正确的 ARIA 属性', () => {
    mockIsLoading.value = true
    const wrapper = mount(NavigationProgress)

    const progressBar = wrapper.find('.navigation-progress')
    expect(progressBar.attributes('role')).toBe('progressbar')
    expect(progressBar.attributes('aria-label')).toBe('Loading')
    expect(progressBar.attributes('aria-valuemin')).toBe('0')
    expect(progressBar.attributes('aria-valuemax')).toBe('100')
  })

  it('应该正确响应 isLoading 状态变化', async () => {
    // 测试初始状态为 false
    mockIsLoading.value = false
    const wrapper = mount(NavigationProgress)
    await wrapper.vm.$nextTick()

    // 初始状态隐藏
    expect(wrapper.find('.navigation-progress').isVisible()).toBe(false)

    // 卸载后重新挂载以测试 true 状态
    wrapper.unmount()

    // 改变为 true 后重新挂载
    mockIsLoading.value = true
    const wrapper2 = mount(NavigationProgress)
    await wrapper2.vm.$nextTick()
    expect(wrapper2.find('.navigation-progress').isVisible()).toBe(true)

    // 清理
    wrapper2.unmount()
  })
})
