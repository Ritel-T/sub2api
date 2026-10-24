import { getCurrentInstance, inject, onScopeDispose, ref, watch, type InjectionKey } from 'vue'
import { useModalContext } from './overlayLifecycle'

export interface ConfirmationContext {
  active: boolean
  identity?: unknown
}

export interface ConfirmationOptions {
  message: string
  title?: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
}

export const ConfirmationSessionKey: InjectionKey<() => ConfirmationContext> = Symbol('confirmation-session')

type ConfirmationAction = (isCurrent: () => boolean) => unknown | Promise<unknown>

export function useConfirmationContext(context?: () => ConfirmationContext): () => ConfirmationContext {
  const inComponent = !!getCurrentInstance()
  const session = inComponent ? inject(ConfirmationSessionKey, undefined) : undefined
  const modal = inComponent ? useModalContext() : undefined
  return () => {
    const local = context?.()
    const currentSession = session?.()
    return {
      active: local?.active !== false && currentSession?.active !== false && modal?.value !== false,
      identity: [local?.identity, currentSession?.identity]
    }
  }
}
interface PendingConfirmation {
  revision: number
  resolve: (accepted: boolean) => void
  reject: (error: unknown) => void
  action?: ConfirmationAction
}

/** A confirmation belongs to one live caller and one unchanged editing context. */
export function useConfirmation(context?: () => ConfirmationContext) {
  const currentContext = useConfirmationContext(context)
  const show = ref(false)
  const message = ref('')
  const title = ref<string>()
  const confirmText = ref<string>()
  const cancelText = ref<string>()
  const danger = ref(false)
  const busy = ref(false)
  let pending: PendingConfirmation | undefined
  let executing: PendingConfirmation | undefined
  let disposed = false
  let revision = 0

  const isCurrent = (session: PendingConfirmation) => !disposed && session.revision === revision && currentContext().active

  function complete(session: PendingConfirmation, accepted: boolean) {
    if (pending !== session) return
    pending = undefined
    show.value = false
    session.resolve(accepted)
  }

  async function execute(session: PendingConfirmation) {
    // Lock before invoking the action: a second confirm cannot start another mutation.
    busy.value = true
    executing = session
    try {
      await session.action?.(() => isCurrent(session))
      complete(session, isCurrent(session))
    } catch (error) {
      if (pending === session) {
        pending = undefined
        show.value = false
        if (isCurrent(session)) session.reject(error)
        else session.resolve(false)
      }
    } finally {
      if (executing === session) {
        executing = undefined
        busy.value = false
      }
    }
  }

  function settle(accepted: boolean) {
    const session = pending
    if (!session || busy.value) return
    if (!accepted || !isCurrent(session)) complete(session, false)
    else if (session.action) void execute(session)
    else complete(session, true)
  }

  function invalidate() {
    revision++
    // Context replacement can close the prompt, including during a request.
    if (pending) complete(pending, false)
  }

  watch(currentContext, invalidate, { deep: true, flush: 'sync' })

  async function start(input: string | ConfirmationOptions, action?: ConfirmationAction): Promise<boolean> {
    if (disposed || pending || busy.value || !currentContext().active) return false
    const options = typeof input === 'string' ? { message: input } : input
    const requestedRevision = revision
    message.value = options.message
    title.value = options.title
    confirmText.value = options.confirmText
    cancelText.value = options.cancelText
    danger.value = options.danger ?? false
    const decision = new Promise<boolean>((resolve, reject) => {
      pending = { revision: requestedRevision, resolve, reject, action }
    })
    show.value = true
    const accepted = await decision
    // Reject a decision accepted immediately before navigation/context replacement.
    return accepted && !disposed && requestedRevision === revision && currentContext().active
  }

  const request = (input: string | ConfirmationOptions) => start(input)
  const run = (options: ConfirmationOptions, action: ConfirmationAction) => start(options, action)

  onScopeDispose(() => { disposed = true; invalidate() })
  return { show, message, title, confirmText, cancelText, danger, busy, request, run, settle }
}
export type Confirmation = ReturnType<typeof useConfirmation>

/** Execution lifecycle for callers that render their own ConfirmDialog. */
export function useConfirmedAction(context?: () => ConfirmationContext) {
  const currentContext = useConfirmationContext(context)
  const busy = ref(false)
  let revision = 0
  let disposed = false
  const invalidate = () => { revision++ }
  watch(currentContext, invalidate, { deep: true, flush: 'sync' })
  onScopeDispose(() => { disposed = true; invalidate() })

  async function run(action: (isCurrent: () => boolean) => unknown | Promise<unknown>): Promise<boolean> {
    if (disposed || busy.value || !currentContext().active) return false
    const requestedRevision = revision
    const isCurrent = () => !disposed && revision === requestedRevision && currentContext().active
    busy.value = true
    try {
      await action(isCurrent)
      return isCurrent()
    } catch (error) {
      if (isCurrent()) throw error
      return false
    } finally {
      // Invalidating a context never releases an in-flight mutation's lock.
      busy.value = false
    }
  }

  return { busy, run, invalidate }
}
