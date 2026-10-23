import { apiClient } from './client'

export type QQChannel = 'email' | 'qq' | 'both'
export interface QQPreferences {
  channel: QQChannel
  balance_low: boolean
  subscription_expiry: boolean
}
export interface QQStatus extends QQPreferences {
  enabled: boolean
  bot_qq: string
  qq: string | null
  pending_qq: string | null
  binding_id: string
  expires_at: string | null
}
export interface QQBindingCode {
  code: string
  expires_at: string
  bot_qq: string
}
export const qqAPI = {
  async status(): Promise<QQStatus> {
    return (await apiClient.get<QQStatus>('/user/qq')).data
  },
  async createBinding(): Promise<QQBindingCode> {
    return (await apiClient.post<QQBindingCode>('/user/qq/binding')).data
  },
  async confirm(qq: string, binding_id: string): Promise<void> {
    await apiClient.post('/user/qq/confirm', { qq, binding_id })
  },
  async unlink(): Promise<void> {
    await apiClient.delete('/user/qq')
  },
  async preferences(preferences: QQPreferences): Promise<void> {
    await apiClient.put('/user/qq/preferences', preferences)
  }
}
