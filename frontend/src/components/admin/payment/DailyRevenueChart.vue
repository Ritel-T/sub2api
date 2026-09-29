<template>
  <div class="card p-4">
    <h3 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">
      {{ t('payment.admin.dailyRevenue') }}
    </h3>
    <div class="h-64">
      <div v-if="loading" class="flex h-full items-center justify-center">
        <LoadingSpinner size="md" />
      </div>
      <Line v-else-if="chartData" :data="chartData" :options="chartOptions" />
      <div
        v-else
        class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400"
      >
        {{ t('payment.admin.noData') }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useChartTheme } from '@/utils/chartTheme'
import { useI18n } from 'vue-i18n'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
} from 'chart.js'
import { Line } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { DailyPaymentStats } from '@/types/payment'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)

const { t } = useI18n()
const chartTheme = useChartTheme()

const props = defineProps<{
  data: DailyPaymentStats[]
  loading?: boolean
}>()

const colors = [
  ['rgb(59, 130, 246)', 'rgba(59, 130, 246, 0.1)'],
  ['rgb(168, 85, 247)', 'rgba(168, 85, 247, 0.1)'],
  ['rgb(245, 158, 11)', 'rgba(245, 158, 11, 0.1)'],
  ['rgb(239, 68, 68)', 'rgba(239, 68, 68, 0.1)'],
]

const chartData = computed(() => {
  if (!props.data || props.data.length === 0) return null
  const currencies = [...new Set(props.data.flatMap(day => Object.keys(day.amount)))].sort()
  return {
    labels: props.data.map(d => d.date),
    datasets: [
      ...currencies.map((currency, index) => {
        const [borderColor, backgroundColor] = colors[index % colors.length]
        return {
          label: `${currency} ${t('payment.admin.revenue')}`,
          data: props.data.map(day => day.amount[currency] || 0),
          borderColor,
          backgroundColor,
          fill: true,
          tension: 0.3,
          pointRadius: 3,
          pointHoverRadius: 5,
        }
      }),
      {
        label: t('payment.admin.orderCount'),
        data: props.data.map(d => d.count),
        borderColor: 'rgb(16, 185, 129)',
        backgroundColor: 'rgba(16, 185, 129, 0.1)',
        fill: false,
        tension: 0.3,
        pointRadius: 3,
        pointHoverRadius: 5,
        yAxisID: 'y1',
      }
    ]
  }
})

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { mode: 'index' as const, intersect: false },
  scales: {
    x: {
      border: { color: chartTheme.value.grid },
      grid: { color: chartTheme.value.grid },
      ticks: { color: chartTheme.value.text }
    },
    y: {
      border: { color: chartTheme.value.grid },
      type: 'linear' as const,
      display: true,
      position: 'left' as const,
      title: { display: true, text: t('payment.admin.revenue'), color: chartTheme.value.text },
      ticks: { color: chartTheme.value.text },
      grid: { color: chartTheme.value.grid },
    },
    y1: {
      border: { color: chartTheme.value.grid },
      type: 'linear' as const,
      display: true,
      position: 'right' as const,
      title: { display: true, text: t('payment.admin.orderCount'), color: chartTheme.value.text },
      ticks: { color: chartTheme.value.text },
      grid: { drawOnChartArea: false, color: chartTheme.value.grid },
    }
  },
  plugins: {
    legend: { position: 'top' as const, labels: { color: chartTheme.value.text } },
    tooltip: { ...chartTheme.value.tooltip },
  }
}))
</script>
