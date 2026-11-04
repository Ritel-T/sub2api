import type { GroupPlatform } from '@/types'

export interface CcsNativeConfiguration {
  file: string
  endpoint: string
  content: string
}

/** Native editor formats from CC Switch v4.0.25; these apps reject provider deep links. */
export function buildCcsNativeConfiguration(input: {
  app: string
  platform: GroupPlatform
  baseUrl: string
  apiKey: string
  model: string
  contextWindow: number
}): CcsNativeConfiguration | null {
  if (!['mcode', 'kimi', 'dsh', 'omp'].includes(input.app)) return null
  const { app, platform, apiKey, model } = input
  let root = input.baseUrl.replace(/\/+$/, '').replace(/\/v1(?:beta)?$/, '')
  if (platform === 'antigravity' && !root.endsWith('/antigravity')) root += '/antigravity'
  const anthropic = platform === 'anthropic' || platform === 'antigravity'
  const responses = ['openai', 'gemini', 'composite'].includes(platform)
  const api = anthropic ? 'anthropic-messages' : responses ? 'openai-responses' : 'openai-completions'
  const endpoint = anthropic ? root : `${root}/v1`
  const quote = (value: string) => JSON.stringify(value)
  const nativeModel = { name: model }
  if (app === 'kimi') {
    const type = anthropic ? 'anthropic' : responses ? 'openai_responses' : 'openai'
    return {
      file: 'config.toml', endpoint,
      content: `[providers.ritel]
type = ${quote(type)}
base_url = ${quote(endpoint)}
api_key = ${quote(apiKey)}

[models.${quote(`ritel/${model}`)}]
provider = "ritel"
model = ${quote(model)}
max_context_size = ${input.contextWindow}
capabilities = ["tool_use"]
`
    }
  }
  if (app === 'omp') {
    return {
      file: 'models.yml', endpoint,
      content: `providers:
  ritel:
    baseUrl: ${quote(endpoint)}
    api: ${quote(api)}
    apiKey: ${quote(apiKey)}
    models:
      - id: ${quote(model)}
        name: ${quote(model)}
`
    }
  }
  const provider = {
    name: 'Ritel.SI API', kind: 'custom', enabled: true, api,
    options: { baseURL: endpoint, apiKey },
    models: { [model]: nativeModel }
  }
  if (app === 'dsh') return { file: 'provider.json', endpoint, content: `${JSON.stringify(provider, null, 2)}\n` }
  return {
    file: 'config.yaml', endpoint,
    content: `custom_provider:
  ritel:
    name: "Ritel.SI API"
    kind: custom
    enabled: true
    api: ${quote(api)}
    options:
      baseURL: ${quote(endpoint)}
      apiKey: ${quote(apiKey)}
    models:
      ${quote(model)}:
        name: ${quote(model)}
`
  }
}
