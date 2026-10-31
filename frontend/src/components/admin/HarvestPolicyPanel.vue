<template>
  <section class="open-section" data-testid="harvest-policy-panel">
    <div class="mb-4 flex items-center justify-between gap-3">
      <AppHeading as="h2" variant="section" >{{ t('consoleInline.automaticHarvestPolicy_bc088e') }}</AppHeading>
      <router-link to="/admin/proxies#harvest" class="text-sm text-primary-600">{{ t('consoleInline.manageHarvestExit_b89c38') }}</router-link>
    </div>
    <p v-if="error" role="alert" class="mb-3 text-sm text-red-600">{{ error }}</p>
    <p v-if="loading" class="text-sm text-gray-500">{{ t('consoleInline.loading_d9e169') }}</p>
    <button v-else-if="!form" type="button" class="btn btn-secondary" @click="load">{{ t('consoleInline.retry_15a6ca') }}</button>
    <form v-else class="space-y-4" @submit.prevent="save">
      <fieldset class="space-y-4" :disabled="saving">
        <div class="flex items-center justify-between gap-4">
          <div class="min-w-0">
            <AppHeading as="h3" variant="subsection" class="text-gray-900 dark:text-white">
              {{ t("admin.settings.gatewayForwarding.codexTicketEnabled") }}
            </AppHeading>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.gatewayForwarding.codexTicketEnabledDesc") }}
            </p>
          </div>
          <Toggle :aria-label="t('admin.settings.gatewayForwarding.codexTicketEnabled')"
            id="codex-ticket-enabled"
            v-model="form.openai_codex_ticket_enabled"
          />
        </div>
        <div class="open-section flex items-center justify-between gap-4">
          <div class="min-w-0">
            <AppHeading as="h3" variant="subsection" class="text-gray-900 dark:text-white">
              {{ t("admin.settings.gatewayForwarding.codexTicketFailClosed") }}
            </AppHeading>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.gatewayForwarding.codexTicketFailClosedDesc") }}
            </p>
          </div>
          <Toggle :aria-label="t('admin.settings.gatewayForwarding.codexTicketFailClosed')"
            id="codex-ticket-fail-closed"
            v-model="form.openai_codex_ticket_fail_closed"
          />
        </div>
        <div class="open-section">
          <AppHeading as="h3" variant="subsection" class="text-gray-900 dark:text-white">
            {{ t("admin.settings.gatewayForwarding.codexTicketModels") }}
          </AppHeading>
          <p :id="'field-' + fieldLabelScope + '-1'" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t("admin.settings.gatewayForwarding.codexTicketModelsDesc") }}
          </p>

          <div class="mt-3 grid gap-3 sm:grid-cols-2">
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                id="codex-ticket-model-astra"
                type="checkbox"
                :checked="form.openai_codex_ticket_models.includes('gpt-6-astra')"
                @change="toggleCodexTicketModel('gpt-6-astra', ($event.target as HTMLInputElement).checked)"
              />
              <span>Astra (gpt-6-astra)</span>
            </label>
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                id="codex-ticket-model-sol"
                type="checkbox"
                :checked="form.openai_codex_ticket_models.includes('gpt-5.6-sol')"
                @change="toggleCodexTicketModel('gpt-5.6-sol', ($event.target as HTMLInputElement).checked)"
              />
              <span>Sol (gpt-5.6-sol)</span>
            </label>
          </div>
          <fieldset class="open-section mt-3">
            <legend class="px-1 text-sm font-semibold">{{ t('consoleInline.automaticHarvestScope_3fc703') }}</legend>
            <select :aria-labelledby="'field-' + fieldLabelScope + '-1'" id="codex-ticket-harvest-scope" v-model="form.openai_codex_ticket_harvest_scope.mode" class="input" @change="changeHarvestScope">
              <option v-if="form.openai_codex_ticket_harvest_scope.mode === 'selected'" value="selected" disabled>{{ t('consoleInline.legacyGroupScopeChooseAnAccountScope_1dca4a') }}</option>
              <option value="all">{{ t('consoleInline.allOAuthAccounts_fa5279') }}</option>
              <option value="selected_accounts">{{ t('consoleInline.selectedOAuthAccounts_b63d23') }}</option>
            </select>
            <p v-if="form.openai_codex_ticket_harvest_scope.mode === 'selected'" class="mt-2 text-xs text-amber-600">{{ t('consoleInline.theSavedGroupScopeRemainsActiveUntil_271d99') }}</p>
            <p v-else-if="form.openai_codex_ticket_harvest_scope.mode === 'all'" class="mt-2 text-xs text-gray-500">{{ t('consoleInline.harvestAllOAuthAccountsExceptThoseManually_32c2dc') }}</p>
            <p v-else-if="form.openai_codex_ticket_harvest_scope.mode === 'selected_accounts'" class="mt-2 text-xs text-gray-500">{{ t('consoleInline.onlyHarvestOAuthAccountsExplicitlyEnabledIn_5961df') }}</p>
            <p class="mt-2 text-xs text-gray-500">{{ t('consoleInline.controlsWhichAccountsAreHarvestedTheExisting_90bac2') }}</p>
          </fieldset>
          <label class="mt-3 block text-sm" for="codex-ticket-strategy">{{ t('consoleInline.ticketRefreshStrategy_34ab53') }}</label>
          <select id="codex-ticket-strategy" v-model="form.openai_codex_ticket_strategy" class="input mt-2">
            <option value="standby">{{ t('consoleInline.prepareStandbyDefault_ef655b') }}</option>
            <option value="fixed">{{ t('consoleInline.keepPrimaryUntilInvalid_2e361d') }}</option>
          </select>
          <div class="open-section mt-3">
            <label class="flex items-center gap-2 text-sm" for="codex-ticket-candy-observation">
              <input id="codex-ticket-candy-observation" type="checkbox" v-model="form.openai_codex_ticket_candy_observation" />
              <span>{{ t('consoleInline.enableCandyTestsToObserveTicketsAfter_64159e') }}</span>
              <span class="rounded bg-amber-100 px-1.5 py-0.5 text-xs text-amber-800 dark:bg-amber-900/30 dark:text-amber-300">Beta</span>
            </label>
            <p class="mt-1 text-xs text-gray-500">{{ t('consoleInline.offByDefaultAfterAnSSEAstra_91e654') }}</p>
          </div>
          <label class="mt-3 flex items-center gap-2 text-sm"><input id="codex-ticket-strict" type="checkbox" v-model="form.openai_codex_ticket_strict_response" />{{ t('consoleInline.rejectMismatchedTicketResponsesOffByDefault_5cf21d') }}</label>
          <p class="mt-1 text-xs text-gray-500">{{ t('consoleInline.rejectsTheBodyWhenHTTPResponseState_d40f15') }}</p>
          <p class="mt-1 text-xs text-gray-500">{{ t('consoleInline.switchingPreservesValidTicketsAndCooldownsFixed_3d3059') }}</p>

        </div>
      </fieldset>
      <button type="submit" data-testid="save-harvest-policy" class="btn btn-primary btn-sm" :disabled="saving">{{ t('consoleInline.saveHarvestPolicy_a93136') }}</button>
    </form>
    <HarvestPoolSchedulingPanel />
  </section>
</template>

<script setup lang="ts">
import { useId as useFieldLabelId } from 'vue'
const fieldLabelScope = useFieldLabelId()

import AppHeading from '@/components/common/AppHeading.vue'
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import HarvestPoolSchedulingPanel from './HarvestPoolSchedulingPanel.vue'
import { getSettings, updateSettings, type SystemSettings } from '@/api/admin/settings'
import { useAppStore } from '@/stores'

const emit = defineEmits<{ saved: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const saving = ref(false)
const loading = ref(true)
const error = ref('')
let disposed = false
function policyOf(settings: SystemSettings) {
  const scope = settings.openai_codex_ticket_harvest_scope
  return {
    openai_codex_ticket_enabled: settings.openai_codex_ticket_enabled ?? false,
    openai_codex_ticket_fail_closed: settings.openai_codex_ticket_fail_closed ?? false,
    openai_codex_ticket_models: [...(settings.openai_codex_ticket_models ?? ['gpt-6-astra', 'gpt-5.6-sol'])],
    openai_codex_ticket_strategy: settings.openai_codex_ticket_strategy || 'standby',
    openai_codex_ticket_candy_observation: settings.openai_codex_ticket_candy_observation ?? false,
    openai_codex_ticket_strict_response: settings.openai_codex_ticket_strict_response ?? false,
    openai_codex_ticket_harvest_scope: {
      mode: scope?.mode || 'all', group_ids: [...(scope?.group_ids ?? [])],
      account_policy: scope?.account_policy || 'schedulable_only'
    }
  }
}
const form = ref<ReturnType<typeof policyOf> | null>(null)
function changeHarvestScope() {
  if (!form.value) return
  form.value.openai_codex_ticket_harvest_scope.group_ids = []
  // Pool scheduling may disable an account until harvesting restores its ticket.
  form.value.openai_codex_ticket_harvest_scope.account_policy = 'prioritize_schedulable'
}
function toggleCodexTicketModel(model: string, checked: boolean) {
  if (!form.value) return
  const models = new Set(form.value.openai_codex_ticket_models)
  if (checked) models.add(model)
  else models.delete(model)
  form.value.openai_codex_ticket_models = [...models]
}
async function load() {
  loading.value = true
  error.value = ''
  const [settings] = await Promise.allSettled([getSettings()])
  if (disposed) return
  if (settings.status === 'fulfilled') form.value = policyOf(settings.value)
  else error.value = t('consoleInline.couldNotLoadHarvestPolicyPleaseRetry_bad4db')
  loading.value = false
}
async function save() {
  if (!form.value || saving.value || loading.value) return
  saving.value = true
  error.value = ''
  try {
    await updateSettings(structuredClone({ ...form.value, openai_codex_ticket_models: [...form.value.openai_codex_ticket_models], openai_codex_ticket_harvest_scope: { ...form.value.openai_codex_ticket_harvest_scope, group_ids: [...form.value.openai_codex_ticket_harvest_scope.group_ids] } }))
    if (disposed) return
    appStore.showSuccess(t('consoleInline.harvestPolicySaved_d1e989'))
    emit('saved')
  } catch {
    if (!disposed) error.value = t('consoleInline.couldNotSaveHarvestPolicyYourChanges_46ca89')
  } finally { if (!disposed) saving.value = false }
}
onMounted(load)
onBeforeUnmount(() => { disposed = true })
</script>
