import { inject, provide, watch, onScopeDispose, computed, type InjectionKey, type Ref } from 'vue'

type Modal = {
  id: symbol
  root: () => HTMLElement | null | undefined
  opener: HTMLElement | null
  escape: () => void
  canEscape: () => boolean
}
type Popover = {
  owner: symbol | null
  root: () => HTMLElement | null | undefined
  trigger: () => HTMLElement | null | undefined
  close: () => void
}
const ownerKey: InjectionKey<symbol> = Symbol('modal-owner')
const activeKey: InjectionKey<Readonly<Ref<boolean>>> = Symbol('modal-active')
export function useModalContext() { return inject(activeKey, computed(() => true)) }
export function provideModalContext(active: Readonly<Ref<boolean>>) { provide(activeKey, active) }
const modals: Modal[] = []
const popovers: Popover[] = []
const selector = 'button, [href], input, select, textarea, [tabindex]'

export function focusableElements(root: HTMLElement | null | undefined): HTMLElement[] {
  return Array.from(root?.querySelectorAll<HTMLElement>(selector) ?? []).filter(element => {
    if (element.tabIndex < 0 || element.matches(':disabled') || element.closest('[hidden], [inert], [aria-hidden="true"]')) return false
    for (let node: HTMLElement | null = element; node; node = node.parentElement) {
      const style = getComputedStyle(node)
      if (style.display === 'none' || style.visibility === 'hidden') return false
    }
    return true
  })
}
function topModal() { return modals[modals.length - 1] }
function modalElements(modal: Modal) {
  const elements = focusableElements(modal.root())
  // A portal belongs at its trigger's position in the keyboard order, not at body end.
  for (const popover of popovers.filter(item => item.owner === modal.id)) {
    const anchor = elements.indexOf(popover.trigger()!)
    const extra = focusableElements(popover.root()).filter(element => !elements.includes(element))
    elements.splice(anchor < 0 ? elements.length : anchor + 1, 0, ...extra)
  }
  return elements
}
function focusModal(modal: Modal) {
  const elements = modalElements(modal)
  ;(elements.find(element => element.hasAttribute('autofocus')) ?? elements[0] ?? modal.root())?.focus()
}
function keydown(event: KeyboardEvent) {
  if (event.defaultPrevented || event.isComposing) return
  const modal = topModal()
  const popover = [...popovers].reverse().find(item => modal ? item.owner === modal.id : item.owner === null)
  if (event.key === 'Escape') {
    if (popover) {
      event.preventDefault()
      const trigger = popover.trigger()
      popover.close()
      if (trigger?.isConnected) trigger.focus()
    } else if (modal?.canEscape()) {
      event.preventDefault()
      modal.escape()
    }
  } else if (event.key === 'Tab' && modal) {
    const elements = modalElements(modal)
    const active = document.activeElement
    let index = elements.indexOf(active as HTMLElement)
    if (index < 0) {
      const panel = popovers.find(item => item.owner === modal.id && item.root() === active)
      const first = panel && focusableElements(panel.root())[0]
      if (first) index = elements.indexOf(first) - (event.shiftKey ? -1 : 1)
    }
    event.preventDefault()
    if (!elements.length) modal.root()?.focus()
    else elements[index < 0 ? (event.shiftKey ? elements.length - 1 : 0) : (index + (event.shiftKey ? -1 : 1) + elements.length) % elements.length]?.focus()

  }
}
function updateListeners() {
  document.body.classList.toggle('modal-open', modals.length > 0)
  document.removeEventListener('keydown', keydown)
  if (modals.length || popovers.length) document.addEventListener('keydown', keydown)
}

export function useModalLifecycle(options: Omit<Modal, 'id' | 'opener'>) {
  const id = Symbol('modal')
  provide(ownerKey, id)
  let registration: Modal | undefined
  function open() {
    if (registration) return
    const active = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const sourcePopover = [...popovers].reverse().find(child => child.root()?.contains(active))
    const opener = sourcePopover?.trigger() ?? active
    for (const child of [...popovers]) child.close()
    registration = { ...options, id, opener }
    modals.push(registration)
    updateListeners()
  }
  function close() {
    if (!registration) return
    const wasTop = topModal() === registration
    const opener = registration.opener
    // Remove children before restoring focus; their owner may remain mounted with show=false.
    for (const child of [...popovers].filter(item => item.owner === id)) {
      popovers.splice(popovers.indexOf(child), 1)
      child.close()
    }
    modals.splice(modals.indexOf(registration), 1)
    options.root()?.setAttribute('inert', '')
    registration = undefined
    updateListeners()
    if (!wasTop) return
    const remaining = topModal()
    if (opener?.isConnected && !opener.matches(':disabled') && !opener.closest('[inert], [hidden]') && (!remaining || remaining.root()?.contains(opener))) opener.focus()
    else if (remaining) focusModal(remaining)
  }
  onScopeDispose(close)
  return { open, close, focus: () => { if (registration) options.root()?.removeAttribute('inert'); if (registration && topModal() === registration) focusModal(registration) } }
}

/** Register interactive overlays even without a modal; standalone popovers retain normal page tab order. */
export function useOwnedPopover(open: Ref<boolean>, options: Omit<Popover, 'owner'>) {
  const owner = inject(ownerKey, null)
  let registration: Popover | undefined
  function unregister() {
    if (!registration) return
    const index = popovers.indexOf(registration)
    if (index >= 0) popovers.splice(index, 1)
    registration = undefined
    updateListeners()
  }
  watch(open, value => {
    options.root()?.toggleAttribute('inert', !value)
    unregister()
    if (value) {
      // Sibling portals (e.g. a selector beside BaseDialog) belong to the modal
      // containing their trigger, even when setup injection cannot cross that sibling.
      const trigger = options.trigger()
      const resolvedOwner = [...modals].reverse().find(modal => trigger && modal.root()?.contains(trigger))?.id ?? owner
      registration = { ...options, owner: resolvedOwner }
      popovers.push(registration)
      updateListeners()
    }
  }, { flush: 'sync' })
  onScopeDispose(unregister)
  return {
    /** Select commits/abandons on Tab: move relative to trigger, never body portal position. */
    tab(event: KeyboardEvent) {
      const modal = topModal()
      if (modal && modal.id !== registration?.owner) return
      const elements = modal ? focusableElements(modal.root()) : focusableElements(document.body).filter(element => !options.root()?.contains(element))
      const anchor = elements.indexOf(options.trigger()!)
      if (anchor < 0) return
      // The search is immediately after its trigger in logical order. Reverse Tab
      // returns there; forward Tab closes the list and advances to the next field.
      const next = event.shiftKey ? elements[anchor] : (elements[anchor + 1] ?? (modal ? elements[0] : undefined))
      if (next) { event.preventDefault(); options.close(); next.focus() }
    }
  }
}
