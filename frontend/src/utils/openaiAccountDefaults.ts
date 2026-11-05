import { oauthModelMappingsError, oauthModelMappingsPayload, type OAuthModelMappingRule } from './oauthModelMappings'

export interface OAuthAccountTemplate {
  concurrency: number
  load_factor: number | null
  priority: number
  model_mappings: OAuthModelMappingRule[]
}
export interface OpenAIAccountDefaultsConfig {
  enabled: boolean
  legacy_account_ids?: number[]
  templates: Record<string, OAuthAccountTemplate>
  model_exclusions?: Record<string, string[]>
  dedicated_proxy_country_codes?: string[]
}
export const OPENAI_TEMPLATE_PLANS = ['free', 'go', 'plus', 'prolite', 'pro', 'promax', 'business'] as const
export function defaultOpenAIAccountDefaults(): OpenAIAccountDefaultsConfig {
  return { enabled: false, templates: {}, model_exclusions: {}, dedicated_proxy_country_codes: [] }
}
export function openaiAccountDefaultsPayload(config: OpenAIAccountDefaultsConfig): OpenAIAccountDefaultsConfig {
  return {
    ...config,
    ...(config.legacy_account_ids ? { legacy_account_ids: [...config.legacy_account_ids] } : {}),
    templates: Object.fromEntries(Object.entries(config.templates ?? {}).map(([plan, template]) => [plan, {
      ...template,
      model_mappings: oauthModelMappingsPayload(template.model_mappings ?? [])
    }])),
    model_exclusions: Object.fromEntries(Object.entries(config.model_exclusions ?? {}).map(([id, models]) => [id, [...models]])),
    dedicated_proxy_country_codes: [...new Set((config.dedicated_proxy_country_codes ?? []).map(code => code.trim().toUpperCase()))]
  }
}
export function openaiAccountDefaultsError(config?: OpenAIAccountDefaultsConfig | null): string | null {
  if (!config) return null
  const templates = Object.entries(config.templates ?? {})
  if (config.enabled && !templates.length) return 'autoConfig.accountDefaults.templatesRequired'
  for (const [, template] of templates) {
    if (!Number.isInteger(template.concurrency) || template.concurrency < 1 || template.concurrency > 10000 ||
        !Number.isInteger(template.priority) || template.priority < 0 || template.priority > 10000 ||
        template.load_factor !== null && (!Number.isInteger(template.load_factor) || template.load_factor < 1 || template.load_factor > 10000)) {
      return 'autoConfig.accountDefaults.invalid'
    }
    if (!template.model_mappings?.length) return 'autoConfig.accountDefaults.modelsRequired'
    const mappingError = oauthModelMappingsError(template.model_mappings)
    if (mappingError) return mappingError
  }
  if ((config.dedicated_proxy_country_codes ?? []).some(code => !/^[A-Za-z]{2}$/.test(code.trim()))) return 'autoConfig.accountDefaults.invalidCountries'
  return null
}
