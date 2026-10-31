import { BRAND_LOGO, BRAND_NAME } from '@/utils/branding'

type StartupLocale = 'en' | 'zh' | 'zh-Hant-HK'
type StartupState = 'loading' | 'offline' | 'failed'

const messages = {
  en: {
    loading: 'Connecting…',
    offline: 'Waiting for a connection. We’ll continue when you’re back online.',
    failed: 'The connection is temporarily unstable. You can try again here.',
    retry: 'Retry connection',
    reconnecting: 'The connection is temporarily unstable. Reconnecting…'
  },
  zh: {
    loading: '正在连接…',
    offline: '正在等待网络连接，恢复后会自动继续。',
    failed: '连接暂时不稳定，可以在这里重试。',
    retry: '重试连接',
    reconnecting: '连接暂时不稳定，正在重试…'
  },
  'zh-Hant-HK': {
    loading: '正在連線…',
    offline: '正在等待網絡連線，恢復後會自動繼續。',
    failed: '連線暫時不穩定，可以在這裡重試。',
    retry: '重試連線',
    reconnecting: '連線暫時不穩定，正在重試…'
  }
} satisfies Record<StartupLocale, Record<StartupState | 'retry' | 'reconnecting', string>>

/** Startup only: no business content, raw session errors or page reloads. */
export function createStartupRecovery(options: {
  root: HTMLElement
  locale: StartupLocale
  finish: (whenActive: () => Promise<void>) => Promise<void>
}) {
  const { root, finish } = options
  const copy = messages[options.locale]
  const shell = document.createElement('div')
  shell.className = 'ritel-startup'
  shell.innerHTML = `
    <header class="ritel-home-header">
      <div class="ritel-home-nav">
        <div class="ritel-brand"><img class="ritel-logo" alt="" width="64" height="50"><span></span></div>
      </div>
    </header>
    <main class="ritel-startup-main">
      <div class="ritel-startup-skeleton" aria-hidden="true"><i></i><i></i><i></i></div>
      <div class="ritel-startup-connection">
        <p role="status" aria-live="polite" aria-atomic="true"></p>
        <button type="button" class="btn btn-secondary" hidden></button>
      </div>
    </main>`
  shell.querySelector('img')!.src = BRAND_LOGO
  shell.querySelector('.ritel-brand span')!.textContent = BRAND_NAME
  const status = shell.querySelector('p')!
  const button = shell.querySelector('button')!
  button.textContent = copy.retry
  root.replaceChildren(shell)

  let running: Promise<void> | undefined
  let disposed = false
  let deferred = false
  let resume: (() => void) | undefined
  let retryTimer: ReturnType<typeof setTimeout> | undefined
  const active = () => document.visibilityState !== 'hidden' && navigator.onLine !== false

  function render(state: StartupState) {
    shell.dataset.state = state
    shell.setAttribute('aria-busy', String(state === 'loading'))
    status.textContent = copy[state]
    button.hidden = state === 'loading'
  }

  // Check between async stages too: a tab may become hidden during hydration.
  function whenActive(): Promise<void> {
    if (active() || disposed) return Promise.resolve()
    if (navigator.onLine === false) render('offline')
    return new Promise(resolve => { resume = resolve })
  }

  function dispose() {
    disposed = true
    clearTimeout(retryTimer)
    window.removeEventListener('online', onResume)
    window.removeEventListener('offline', onOffline)
    document.removeEventListener('visibilitychange', onResume)
    button.removeEventListener('click', onRetry)
    resume?.()
    resume = undefined
  }

  function retry(background = false): Promise<void> {
    if (disposed) return Promise.resolve()
    if (running) return running
    if (!active()) {
      deferred = true
      if (navigator.onLine === false) render('offline')
      return Promise.resolve()
    }
    deferred = false
    clearTimeout(retryTimer)
    if (background && shell.dataset.state !== 'loading') {
      shell.setAttribute('aria-busy', 'true')
      status.textContent = copy.reconnecting
    } else {
      render('loading')
    }
    running = Promise.resolve()
      .then(() => finish(whenActive))
      .then(() => { dispose() })
      .catch(() => {
        if (!disposed) {
          render(navigator.onLine === false ? 'offline' : 'failed')
          // One quiet attempt per 30 seconds after persistent failure. The inner
          // account initializer owns its short bounded network retries.
          retryTimer = setTimeout(() => { void retry(true) }, 30_000)
        }
      })
      .finally(() => { running = undefined })
    return running
  }

  function onRetry() { void retry(true) }
  function onOffline() {
    if (!disposed) render('offline')
  }
  function onResume(event: Event) {
    if (disposed) return
    if (!active()) {
      if (event.type === 'online') deferred = true
      return
    }
    if (resume) {
      const resolve = resume
      resume = undefined
      deferred = false
      resolve()
      shell.setAttribute('aria-busy', 'true')
      status.textContent = button.hidden ? copy.loading : copy.reconnecting
      return
    }
    // Visibility changes only resume work actually deferred while hidden/offline.
    // Online events retry immediately; in-flight work is always coalesced.
    if (deferred || event.type === 'online') {
      if (running) return
      void retry(true)
    }
  }

  render(navigator.onLine === false ? 'offline' : 'loading')
  button.addEventListener('click', onRetry)
  window.addEventListener('online', onResume)
  window.addEventListener('offline', onOffline)
  document.addEventListener('visibilitychange', onResume)
  return { retry, dispose }
}
