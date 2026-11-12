<template>
  <div class="text-xs text-muted-foreground space-y-1.5">
    <div v-if="range.start" class="flex flex-wrap justify-between gap-x-4 gap-y-1">
      <span>{{ bill.rent_start_date ? '租金计费日期' : '账期覆盖日期' }}</span>
      <span class="tabular-nums text-right">
        <span class="block whitespace-nowrap">起 {{ range.start }}</span>
        <span class="block whitespace-nowrap">止 {{ range.end }}</span>
      </span>
    </div>
    <p v-if="bill.rent_basis_source === 'current_lease_reference'" class="text-[11px]">历史账单未保存计算快照，以下按当前租约回算供核对，原账单金额保持。</p>
    <details v-if="bill.rent_calculation" class="text-[11px]" open>
      <summary class="cursor-pointer">租金计算明细</summary>
      <p class="whitespace-pre-line break-words leading-relaxed mt-1">{{ bill.rent_calculation }}</p>
      <p v-if="manualAmount" class="mt-1 text-amber-600 dark:text-amber-300">当前录入 {{ Number(bill.rent || 0).toFixed(2) }} 元，与自动计算参考金额不同，请核对。</p>
    </details>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { rentRange, type RentBasisBill } from '../utils/bill-dates'
const props = defineProps<{ bill: RentBasisBill }>()
const range = computed(() => rentRange(props.bill))
const manualAmount = computed(() => props.bill.rent_basis_amount != null && Math.abs((props.bill.rent || 0)-props.bill.rent_basis_amount) > 0.005)
</script>
