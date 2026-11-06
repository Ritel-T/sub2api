/** CC Switch provider deeplink profiles published in public site settings. */
export type CcsImportPlatform = 'anthropic' | 'openai' | 'gemini' | 'antigravity' | 'grok' | 'deepseek' | 'minimax' | 'kimi' | 'zhipu' | 'opencode_go' | 'composite'

export type CcsImportApp = 'claude' | 'codex' | 'gemini' | 'agy' | 'grokbuild' | 'opencode' | 'openclaw' | 'hermes' | 'pi' | 'claude-desktop' | 'mcode' | 'kimi' | 'dsh' | 'omp'

export type CcsImportParameterValue = string | boolean | number

export interface CcsImportProfile {
  id: string
  label: string
  platform: CcsImportPlatform
  app: CcsImportApp
  /** Raw config and usageScript are encoded by the deeplink builder. */
  parameters: Record<string, CcsImportParameterValue>
}

export interface CcsImportConfig {
  /** Order controls the user's import menu. An empty list disables import options. */
  profiles: CcsImportProfile[]
}
