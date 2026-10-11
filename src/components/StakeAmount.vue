<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Settings2 } from 'lucide-vue-next'
import { user, money } from '../api/account'
import { defaultStakes, parseStake, stakeText } from '../lib/stake'
const props = defineProps<{ modelValue: string; min: number; max: number; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const presets = ref([...defaultStakes]), editing = ref(false), draft = ref<string[]>([]), error = ref(''), saved = ref('')
const storageKey = computed(() => `starlight.quick-stakes.v1.${user.value?.id ?? 'guest'}`)
watch(storageKey, () => {
  presets.value = [...defaultStakes]; editing.value = false; saved.value = ''
  try {
    const list: unknown = JSON.parse(localStorage.getItem(storageKey.value) ?? 'null')
    if (Array.isArray(list) && list.length === 6 && list.every(n => Number.isSafeInteger(n) && n > 0 && n <= 100000000) && new Set(list).size === 6) presets.value = list
  } catch { /* Keep usable defaults if storage is unavailable. */ }
}, { immediate: true })
const amount = computed(() => parseStake(props.modelValue))
const amountError = computed(() => !amount.value ? '请输入有效金额，最多两位小数' : amount.value < props.min || amount.value > props.max ? `投注范围 ¥${money(props.min)}–¥${money(props.max)}` : '')
function edit() { draft.value = presets.value.map(stakeText); error.value = ''; saved.value = ''; editing.value = true }
function save() {
  const next = draft.value.map(parseStake)
  if (next.some(n => !n) || new Set(next).size !== 6) { error.value = '请输入6个不同的正数金额，最多两位小数'; return }
  try { localStorage.setItem(storageKey.value, JSON.stringify(next)) }
  catch { error.value = '浏览器暂时无法保存设置，请检查存储权限后重试'; return }
  presets.value = next; editing.value = false; saved.value = '快捷金额已保存'
}
</script>
<template>
  <section class="stake-control">
    <div class="stake-heading"><label>投注金额 <span>元</span><input :value="modelValue" :disabled="disabled" inputmode="decimal" aria-label="投注金额（元）" placeholder="输入金额" @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)" /></label><button type="button" class="text-button" :disabled="disabled" :aria-expanded="editing" @click="editing ? editing=false : edit()"><Settings2 :size="15"/>自定义快捷金额</button></div>
    <div class="stake-shortcuts" aria-label="快捷投注金额"><button v-for="n in presets" :key="n" type="button" :disabled="disabled || n < min || n > max" :class="{active:amount===n}" :aria-pressed="amount===n" :aria-label="`快捷投注${stakeText(n)}元`" :title="n<min || n>max ? '超出本场投注范围' : `${stakeText(n)}元`" @click="emit('update:modelValue',stakeText(n))">{{stakeText(n)}}</button></div>
    <p v-if="amountError" class="stake-error" role="alert">{{amountError}}</p><p v-if="saved" class="stake-saved" role="status">{{saved}}</p>
    <div v-if="editing" class="stake-editor"><div class="stake-editor-heading"><strong>我的快捷金额</strong><small>设置6个常用金额</small></div><div class="stake-editor-inputs"><label v-for="(_,i) in draft" :key="i"><span>{{i+1}}</span><input v-model="draft[i]" :disabled="disabled" inputmode="decimal" :aria-label="`快捷金额${i+1}`"/></label></div><p v-if="error" class="stake-error" role="alert">{{error}}</p><div class="stake-editor-actions"><button type="button" class="text-button" :disabled="disabled" @click="draft=defaultStakes.map(stakeText);error=''">恢复默认</button><button type="button" class="secondary-button" :disabled="disabled" @click="editing=false">取消</button><button type="button" class="primary-button" :disabled="disabled" @click="save">保存快捷金额</button></div></div>
  </section>
</template>
