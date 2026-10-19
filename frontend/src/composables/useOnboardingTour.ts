import { onMounted, onUnmounted, nextTick } from 'vue'
import { driver, type Driver, type DriveStep } from 'driver.js'
import 'driver.js/dist/driver.css'
import { useAuthStore as useUserStore } from '@/stores/auth'
import { useOnboardingStore } from '@/stores/onboarding'
import { useI18n } from 'vue-i18n'
import { getAdminSteps, getUserSteps } from '@/components/Guide/steps'

export interface OnboardingOptions {
  storageKey?: string
  autoStart?: boolean
  prepareElement?: (selector: string) => Promise<boolean>
  onEnd?: () => void
}

// Route views each mount an AppLayout. The active driver's owner keeps the
// session, and replacement layouts delegate to it rather than advancing a
// second driver or cancelling an in-flight navigation.
let startVersion = 0
const sessions = new WeakMap<Driver, { moveBy: (offset: number, delay?: number) => Promise<void> }>()

export function useOnboardingTour(options: OnboardingOptions) {
  const { t } = useI18n()
  const userStore = useUserStore()
  const onboardingStore = useOnboardingStore()
  const storageVersion = 'v4_interactive'

  const TIMING = {
    INTERACTIVE_WAIT_MS: 800,
    ELEMENT_TIMEOUT_MS: 8000,
    AUTO_START_DELAY_MS: 1000
  } as const

  const isInteractiveStep = (step: DriveStep): boolean =>
    step.popover?.showButtons?.length === 1 && step.popover.showButtons[0] === 'close'

  const cleanupClickListener = () => {
    if (!currentClickListener) return
    const { element: el, handler, keyHandler, originalTabIndex, eventTypes } = currentClickListener
    eventTypes.forEach(type => el.removeEventListener(type, handler))
    if (keyHandler) el.removeEventListener('keydown', keyHandler)
    if (originalTabIndex !== undefined) {
      if (originalTabIndex === null) el.removeAttribute('tabindex')
      else el.setAttribute('tabindex', originalTabIndex)
    }
    currentClickListener = null
  }

  let driverInstance: Driver | null = onboardingStore.getDriverInstance()
  let currentClickListener: {
    element: HTMLElement
    handler: () => void
    keyHandler?: (e: KeyboardEvent) => void
    originalTabIndex?: string | null
    eventTypes: string[]
  } | null = null
  let autoStartTimer: ReturnType<typeof setTimeout> | null = null
  let globalKeyboardHandler: ((e: KeyboardEvent) => void) | null = null

  const getStorageKey = () => {
    const baseKey = options.storageKey ?? 'onboarding_tour'
    const userId = userStore.user?.id ?? 'guest'
    const role = userStore.user?.role ?? 'user'
    return `${baseKey}_${userId}_${role}_${storageVersion}`
  }

  const hasSeen = () => {
    return localStorage.getItem(getStorageKey()) === 'true'
  }

  const markAsSeen = () => {
    localStorage.setItem(getStorageKey(), 'true')
  }

  const clearSeen = () => {
    localStorage.removeItem(getStorageKey())
  }

  const ensureElement = async (selector: string, timeout = 5000, valid = () => true): Promise<boolean> => {
    const startTime = Date.now()
    while (valid() && Date.now() - startTime < timeout) {
      const element = document.querySelector(selector)
      if (element && !element.closest('[inert], [aria-hidden="true"]') && element.getBoundingClientRect().height > 0) {
        return true
      }
      await new Promise((resolve) => setTimeout(resolve, 150))
    }
    return false
  }

  const startTour = async (startIndex = 0) => {
    const requestedStart = ++startVersion
    const isAdmin = userStore.user?.role === 'admin'
    const isSimpleMode = userStore.isSimpleMode
    const steps = isAdmin ? getAdminSteps(t, isSimpleMode) : getUserSteps(t)

    await nextTick()

    if (requestedStart !== startVersion) return
    // End the old session before preparing the next drawer target, so its
    // cleanup cannot close the new session's navigation surface.
    driverInstance?.destroy()

    const currentStep = steps[startIndex]
    if (currentStep?.element && typeof currentStep.element === 'string') {
      if (await options.prepareElement?.(currentStep.element) === false) return
      if (!await ensureElement(currentStep.element, TIMING.ELEMENT_TIMEOUT_MS)) return
    }

    if (requestedStart !== startVersion) return

    const interactiveTarget = () => {
      const step = steps[sessionDriver.getActiveIndex() ?? 0]
      if (!step || !isInteractiveStep(step) || !step.element) return null
      const element = typeof step.element === 'string'
        ? document.querySelector<HTMLElement>(step.element)
        : step.element as HTMLElement
      return element && !['INPUT', 'TEXTAREA', 'SELECT'].includes(element.tagName) ? element : null
    }
    const finishTour = () => {
      markAsSeen()
      sessionDriver.destroy()
    }

    let moving = false
    let sessionEnded = false
    const moveBy = async (offset: number, delay = 0) => {
      if (moving || !sessionDriver?.isActive()) return
      moving = true
      const index = sessionDriver.getActiveIndex() ?? 0
      const targetIndex = index + offset
      const valid = () => sessionDriver.isActive() && onboardingStore.getDriverInstance() === sessionDriver && sessionDriver.getActiveIndex() === index
      try {
        if (delay) await new Promise(resolve => setTimeout(resolve, delay))
        if (!valid()) return
        const step = steps[targetIndex]
        if (!step) {
          if (targetIndex >= steps.length) {
            finishTour()
          }
          return
        }
        if (typeof step.element === 'string') {
          if (await options.prepareElement?.(step.element) === false || !valid()) return
          if (!await ensureElement(step.element, TIMING.ELEMENT_TIMEOUT_MS, valid)) return
        }
        if (valid()) sessionDriver.moveTo(targetIndex)
      } catch (error) {
        console.warn('Onboarding: Could not prepare the next target', error)
      } finally {
        moving = false
      }
    }

    const sessionDriver = driver({
      showProgress: true,
      steps,
      animate: true,
      allowClose: false, // 禁止点击遮罩关闭
      stagePadding: 4,
      popoverClass: 'theme-tour-popover',
      nextBtnText: t('common.next'),
      prevBtnText: t('common.back'),
      doneBtnText: t('common.confirm'),

      onNextClick: (_el, _step, { config, state }) => {
        if (state.activeIndex === (config.steps?.length ?? 0) - 1) {
          finishTour()
          return
        }
        const target = interactiveTarget()
        if (target) target.click()
        else void moveBy(1)
      },
      onPrevClick: () => { void moveBy(-1) },
      onCloseClick: finishTour,

      onPopoverRender: (popover, { config, state }) => {
        try {
          const { title: titleEl, footer: footerEl, nextButton, previousButton } = popover

          if (!titleEl || !footerEl) {
            console.warn('Onboarding: Missing popover elements')
            return
          }

          const currentStep = steps[state.activeIndex ?? 0]

          if (currentStep && isInteractiveStep(currentStep) && popover.description) {
            const hintClass = 'driver-popover-description-hint'
            if (!popover.description.querySelector(`.${hintClass}`)) {
              const hint = document.createElement('div')
              hint.className = `${hintClass} mt-2 text-xs text-gray-500 flex items-center gap-1`

              const iconSpan = document.createElement('span')
              iconSpan.className = 'i-mdi-keyboard-return mr-1'

              const textNode = document.createTextNode(
                t('onboarding.interactiveHint', 'Press Enter or Click to continue'),
              )

              hint.appendChild(iconSpan)
              hint.appendChild(textNode)
              popover.description.appendChild(hint)
            }
          }

          if (!footerEl.classList.contains('reorganized')) {
            footerEl.classList.add('reorganized')

            const progressEl = footerEl.querySelector('.driver-popover-progress-text')
            const nextBtnEl = nextButton || footerEl.querySelector('.driver-popover-next-btn')
            const prevBtnEl = previousButton || footerEl.querySelector('.driver-popover-prev-btn')

            const leftContainer = document.createElement('div')
            leftContainer.className = 'footer-left'

            const rightContainer = document.createElement('div')
            rightContainer.className = 'footer-right'

            if (progressEl) leftContainer.appendChild(progressEl)

            const shortcutsEl = document.createElement('div')
            shortcutsEl.className = 'footer-shortcuts'

            const shortcut1 = document.createElement('span')
            shortcut1.className = 'shortcut-item'
            const kbd1 = document.createElement('kbd')
            kbd1.textContent = '←'
            const kbd2 = document.createElement('kbd')
            kbd2.textContent = '→'
            shortcut1.appendChild(kbd1)
            shortcut1.appendChild(kbd2)
            shortcut1.appendChild(
              document.createTextNode(` ${t('onboarding.navigation.flipPage')}`),
            )

            const shortcut2 = document.createElement('span')
            shortcut2.className = 'shortcut-item'
            const kbd3 = document.createElement('kbd')
            kbd3.textContent = 'ESC'
            shortcut2.appendChild(kbd3)
            shortcut2.appendChild(
              document.createTextNode(` ${t('onboarding.navigation.exit')}`),
            )

            shortcutsEl.appendChild(shortcut1)
            shortcutsEl.appendChild(shortcut2)
            leftContainer.appendChild(shortcutsEl)

            if (prevBtnEl) rightContainer.appendChild(prevBtnEl)
            if (nextBtnEl) rightContainer.appendChild(nextBtnEl)

            footerEl.innerHTML = ''
            footerEl.appendChild(leftContainer)
            footerEl.appendChild(rightContainer)
          }

          const isLastStep = state.activeIndex === (config.steps?.length ?? 0) - 1
          const activeNextBtn = nextButton || footerEl.querySelector('.driver-popover-next-btn')

          activeNextBtn?.classList.toggle('driver-popover-done-btn', isLastStep)
        } catch (e) {
          console.error('Onboarding Tour Render Error:', e)
        }
      },

      onHighlightStarted: (element, step) => {
        cleanupClickListener()

        if (isInteractiveStep(step) && element) {
          const htmlElement = element as HTMLElement

          // Submit steps advance only after business validation succeeds.
          const isSubmitButton = htmlElement.getAttribute('type') === 'submit' ||
                                (htmlElement.tagName === 'BUTTON' && htmlElement.closest('form'))

          if (isSubmitButton) {
            return
          }

          const isSelectComponent = htmlElement.querySelector('.select-trigger') !== null ||
                                    htmlElement.classList.contains('select-trigger')

          // Teleported Select options do not bubble here; navigation stays manual.
          if (isSelectComponent) {
            return
          }

          const originalTabIndex = htmlElement.getAttribute('tabindex')
          if (!htmlElement.isContentEditable && htmlElement.tabIndex === -1) {
            htmlElement.setAttribute('tabindex', '0')
          }

          let hasExecuted = false

          const boundStepIndex = sessionDriver.getActiveIndex() ?? 0

          const clickHandler = async () => {
            if (hasExecuted || !sessionDriver.isActive() || sessionDriver.getActiveIndex() !== boundStepIndex) return
            hasExecuted = true
            await moveBy(1, TIMING.INTERACTIVE_WAIT_MS)
          }

          const isInputField = ['INPUT', 'TEXTAREA', 'SELECT'].includes(htmlElement.tagName)

          if (isInputField) {
            const inputHandler = () => {
              htmlElement.removeEventListener('input', inputHandler)
              htmlElement.removeEventListener('change', inputHandler)
              clickHandler()
            }

            htmlElement.addEventListener('input', inputHandler)
            htmlElement.addEventListener('change', inputHandler)

            currentClickListener = {
              element: htmlElement,
              handler: inputHandler,
              originalTabIndex,
              eventTypes: ['input', 'change']
            }
          } else {
            const keyHandler = (e: KeyboardEvent) => {
              if (['Enter', ' '].includes(e.key)) {
                e.preventDefault()
                htmlElement.click()
              }
            }

            htmlElement.addEventListener('click', clickHandler, { once: true })
            htmlElement.addEventListener('keydown', keyHandler)

            currentClickListener = {
              element: htmlElement,
              handler: clickHandler,
              keyHandler,
              originalTabIndex,
              eventTypes: ['click']
            }
          }
        }
      },

      onDestroyed: () => {
        cleanupClickListener()
        if (globalKeyboardHandler) {
          document.removeEventListener('keydown', globalKeyboardHandler, { capture: true })
          globalKeyboardHandler = null
        }
        sessions.delete(sessionDriver)
        if (!sessionEnded) {
          sessionEnded = true
          options.onEnd?.()
        }
        if (onboardingStore.getDriverInstance() === sessionDriver) onboardingStore.setDriverInstance(null)
      }
    })

    driverInstance = sessionDriver
    sessions.set(sessionDriver, { moveBy })
    onboardingStore.setDriverInstance(driverInstance)

    globalKeyboardHandler = (e: KeyboardEvent) => {
      if (!sessionDriver.isActive()) return
      const key = e.key
      if (!['Escape', 'ArrowRight', 'Enter', 'ArrowLeft'].includes(key)) return
      const target = e.target as HTMLElement
      if (key !== 'Escape' && ['INPUT', 'TEXTAREA'].includes(target?.tagName)) return
      if (key === 'ArrowLeft' && (target?.tagName === 'SELECT' || target?.isContentEditable)) return

      e.preventDefault()
      e.stopPropagation()
      if (key === 'Escape') {
        finishTour()
      } else if (key === 'ArrowLeft') {
        void moveBy(-1)
      } else {
        const element = interactiveTarget()
        if (element) {
          if (key === 'Enter') element.click()
        } else {
          void moveBy(1)
        }
      }
    }

    document.addEventListener('keydown', globalKeyboardHandler, { capture: true })
    driverInstance.drive(startIndex)
  }

  const nextStep = async (delay = 300) => {
    const active = onboardingStore.getDriverInstance()
    if (active?.isActive()) await sessions.get(active)?.moveBy(1, delay)
  }

  const isCurrentStep = (elementSelector: string): boolean => {
    if (!driverInstance?.isActive()) return false
    const activeElement = driverInstance.getActiveElement()
    return activeElement?.matches(elementSelector) ?? false
  }

  const replayTour = () => {
    clearSeen()
    void startTour()
  }

  onMounted(async () => {
    onboardingStore.setControlMethods({
      nextStep,
      isCurrentStep
    })

    if (onboardingStore.isDriverActive()) {
      driverInstance = onboardingStore.getDriverInstance()
      return
    }

    if (userStore.isSimpleMode || userStore.user?.role !== 'admin' || !options.autoStart || hasSeen()) return
    autoStartTimer = setTimeout(() => {
      void startTour()
    }, TIMING.AUTO_START_DELAY_MS)
  })

  onUnmounted(() => {
    if (autoStartTimer) {
      clearTimeout(autoStartTimer)
      autoStartTimer = null
    }
    // The driver owns its listeners across route view replacement.
    onboardingStore.clearControlMethods()
  })

  return {
    startTour,
    replayTour,
    nextStep,
    isCurrentStep,
    hasSeen,
    markAsSeen,
    clearSeen
  }
}
