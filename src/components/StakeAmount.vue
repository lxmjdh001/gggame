<script setup lang="ts">
import { computed } from 'vue'
import { money } from '../api/account'
import { parseStake } from '../lib/stake'
const props = defineProps<{ modelValue: string; min: number; max: number; disabled?: boolean; label?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const amount = computed(() => parseStake(props.modelValue))
const amountError = computed(() => !amount.value ? '请输入有效金额，最多两位小数' : amount.value < props.min || amount.value > props.max ? `投注范围 ¥${money(props.min)}–¥${money(props.max)}` : '')
</script>
<template>
  <section class="stake-control">
    <div class="stake-heading"><label>{{label??'投注金额'}} <span>元</span><input :value="modelValue" :disabled="disabled" inputmode="decimal" :aria-label="`${label??'投注金额'}（元）`" placeholder="输入金额" @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)" /></label></div>
    <p v-if="amountError" class="stake-error" role="alert">{{amountError}}</p>
  </section>
</template>
