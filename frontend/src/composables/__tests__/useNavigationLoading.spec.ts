// @vitest-environment node
/**
 * useNavigationLoading 组合式函数单元测试
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  useNavigationLoading,
  _resetNavigationLoadingInstance
} from '../useNavigationLoading'

describe('useNavigationLoading', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    _resetNavigationLoadingInstance()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  describe('startNavigation', () => {

    it('导航开始后延迟显示加载指示器（防闪烁）', () => {
      const { isLoading, startNavigation, ANTI_FLICKER_DELAY } = useNavigationLoading()

      startNavigation()

      // 立即检查，不应该显示
      expect(isLoading.value).toBe(false)

      // 经过防闪烁延迟后应该显示
      vi.advanceTimersByTime(ANTI_FLICKER_DELAY)
      expect(isLoading.value).toBe(true)
    })
  })

  describe('endNavigation', () => {
    it('导航结束时 isLoading 应变为 false', () => {
      const { isLoading, startNavigation, endNavigation, ANTI_FLICKER_DELAY } = useNavigationLoading()

      startNavigation()
      vi.advanceTimersByTime(ANTI_FLICKER_DELAY)
      expect(isLoading.value).toBe(true)

      endNavigation()
      expect(isLoading.value).toBe(false)
    })
  })

  describe('快速导航（< 100ms）防闪烁', () => {
    it('快速导航不应触发显示加载指示器', () => {
      const { isLoading, startNavigation, endNavigation, ANTI_FLICKER_DELAY } = useNavigationLoading()

      startNavigation()

      // 在防闪烁延迟之前结束导航
      vi.advanceTimersByTime(ANTI_FLICKER_DELAY - 50)
      endNavigation()

      // 不应该显示加载指示器
      expect(isLoading.value).toBe(false)

      // 即使继续等待也不应该显示
      vi.advanceTimersByTime(100)
      expect(isLoading.value).toBe(false)
    })
  })

  describe('cancelNavigation', () => {
    it('导航取消时应正确重置状态', () => {
      const { isLoading, startNavigation, cancelNavigation, ANTI_FLICKER_DELAY } = useNavigationLoading()

      startNavigation()
      vi.advanceTimersByTime(ANTI_FLICKER_DELAY / 2)

      cancelNavigation()

      // 取消后不应该触发显示
      vi.advanceTimersByTime(ANTI_FLICKER_DELAY)
      expect(isLoading.value).toBe(false)
    })
  })

  describe('getNavigationDuration', () => {
    it('应该返回正确的导航持续时间', () => {
      const { startNavigation, getNavigationDuration } = useNavigationLoading()

      expect(getNavigationDuration()).toBeNull()

      startNavigation()
      vi.advanceTimersByTime(500)

      const duration = getNavigationDuration()
      expect(duration).toBe(500)
    })
  })

  describe('resetState', () => {
    it('应该重置所有状态', () => {
      const { isLoading, isNavigating, startNavigation, resetState, ANTI_FLICKER_DELAY } = useNavigationLoading()

      startNavigation()
      vi.advanceTimersByTime(ANTI_FLICKER_DELAY)

      expect(isLoading.value).toBe(true)
      expect(isNavigating.value).toBe(true)

      resetState()

      expect(isLoading.value).toBe(false)
      expect(isNavigating.value).toBe(false)
    })
  })

  describe('连续导航场景', () => {
    it('连续快速导航应正确处理状态', () => {
      const { isLoading, startNavigation, cancelNavigation, endNavigation, ANTI_FLICKER_DELAY } = useNavigationLoading()

      // 第一次导航
      startNavigation()
      vi.advanceTimersByTime(30)

      // 第二次导航（取消第一次）
      cancelNavigation()
      startNavigation()
      vi.advanceTimersByTime(30)

      // 第三次导航（取消第二次）
      cancelNavigation()
      startNavigation()

      // 这次等待足够长时间
      vi.advanceTimersByTime(ANTI_FLICKER_DELAY)
      expect(isLoading.value).toBe(true)

      // 结束导航
      endNavigation()
      expect(isLoading.value).toBe(false)
    })
  })
})
