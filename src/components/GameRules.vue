<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { BookOpen, ChevronRight, X } from 'lucide-vue-next'
import type { Game } from '../data'
import { getJSON, type Session } from '../api/games'
import type { MarketBook } from '../api/markets'
const props = defineProps<{ game: Game; session: Session }>()
const dialog = ref<HTMLDialogElement | null>(null)
const book = ref<MarketBook | null>(null), loading = ref(false), error = ref('')
let controller: AbortController | null = null
let previousOverflow: string | null = null
const introduction = computed(() => ({
 ssc: '每期由 0–9 中的 5 个不重复号码组成，依次对应第一球至第五球。',
 ffc: '每期由 5 个 0–9 号码组成，号码可重复，依次对应第一球至第五球。',
 k3: '每期由 3 个 1–6 点数组成，点数可重复。可选择单球、总和大小单双或指定数字。',
 pk10: '每期将 01–10 排列为十个名次，号码不重复。按所选名次或前两名号码之和判定结果。',
 lhc: '每期从 01–49 中开出 7 个不同号码，前 6 位为正码，第 7 位为特别号。',
 pc28: '每期开出 3 个 0–9 号码，号码可重复，三个号码相加得到 0–27 的和值。',
} as Record<string, string>)[props.game.id])
const sections = computed(() => (['two_sides', 'regular'] as const).map(category => {
 const rules = new Map<string, Set<string>>()
 for (const market of book.value?.markets ?? []) {
  if (market.category !== category || !market.rule) continue
  if (!rules.has(market.rule)) rules.set(market.rule, new Set())
  rules.get(market.rule)!.add(market.group)
 }
 return { title: category === 'two_sides' ? '两面玩法' : '常规玩法', items: [...rules].map(([text, groups]) => ({text, groups: [...groups].join('、')})) }
}))
async function load() {
 controller?.abort()
 const current = new AbortController(); controller = current
 loading.value = true; error.value = ''; book.value = null
 const timeout = setTimeout(() => current.abort(), 8000)
 try {
  const data = await getJSON<MarketBook>(`/api/v1/markets/${encodeURIComponent(props.session.id)}`, current.signal)
  if (controller !== current) return
  if (data.session_id !== props.session.id || !Array.isArray(data.markets)) throw new Error('玩法信息暂不可用')
  book.value = data
 } catch (e) {
  if (controller === current) error.value = current.signal.aborted ? '加载超时，请重试' : (e as Error).message
 } finally { clearTimeout(timeout); if (controller === current) { loading.value = false; controller = null } }
}
function open() {
 dialog.value?.showModal()
 previousOverflow = document.body.style.overflow
 document.body.style.overflow = 'hidden'
 void load()
}
function cleanup() {
 controller?.abort(); controller = null
 if (previousOverflow !== null) { document.body.style.overflow = previousOverflow; previousOverflow = null }
}
function close() { dialog.value?.close(); cleanup() }
function backdrop(event: MouseEvent) {
 if (!dialog.value || event.target !== dialog.value) return
 const rect = dialog.value.getBoundingClientRect()
 if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) close()
}
onUnmounted(cleanup)
</script>
<template>
 <button class="view-rules" aria-haspopup="dialog" @click="open">查看玩法<ChevronRight :size="17"/></button>
 <Teleport to="body">
  <dialog ref="dialog" class="rules-dialog" aria-labelledby="rules-title" @close="cleanup" @click="backdrop">
   <header class="rules-header"><div><span><BookOpen :size="16"/>玩法简介</span><h2 id="rules-title">{{ game.name }}</h2></div><button class="icon-button" aria-label="关闭玩法简介" autofocus @click="close"><X :size="21"/></button></header>
   <div class="rules-content">
    <section class="rules-overview"><h3>{{ session.name }}</h3><p>{{ introduction }}</p><p>{{ game.id==='lhc'?'每日 21:30:05 开奖，21:29:50 截止投注。':`每 ${session.interval_minutes} 分钟一期，开奖分钟的第 05 秒开奖，提前10分钟截止下注。` }}</p></section>
    <p v-if="loading" role="status">正在加载玩法…</p>
    <div v-else-if="error" class="rules-error" role="alert">{{ error }}<button class="text-button" @click="load">重试</button></div>
    <template v-else><section v-for="section in sections" :key="section.title" class="rules-section"><h3>{{ section.title }}</h3><p v-if="section.title==='常规玩法'">先选择固定位置，再选择该位置的大小单双或指定数字；大小单双判定与两面玩法一致。</p><article v-for="item in section.items" :key="item.text"><h4>{{ item.groups }}</h4><p>{{ item.text }}</p></article></section></template>
    <section v-if="game.id==='lhc'" class="rules-section"><h3>六号单式</h3><p>从 01–49 中选择 6 个不同号码，顺序不限。按所选号码与正码、特别号的匹配情况判奖，由管理员核对开奖结果后结算。</p><p>匹配参考：6 个正码；5 个正码＋特别号；5 个正码；4 个正码＋特别号；4 个正码；3 个正码＋特别号；3 个正码。</p></section>
   </div>
   <footer class="rules-footer"><button class="primary-button" @click="close">知道了</button></footer>
  </dialog>
 </Teleport>
</template>
<style scoped>
.view-rules{display:flex;align-items:center;gap:6px;margin-left:auto;color:#9675c0;font-size:12px;padding:8px 0 8px 12px;white-space:nowrap}
.rules-dialog{width:620px;max-width:calc(100vw - 32px);max-height:85dvh;padding:0;border:1px solid #e9e2f4;border-radius:20px;color:#514b68;background:#fff;box-shadow:0 24px 90px #34234833;overflow:hidden}
.rules-dialog[open]{display:flex;flex-direction:column}.rules-dialog::backdrop{background:#2f244660;backdrop-filter:blur(4px)}
.rules-header{display:flex;align-items:center;justify-content:space-between;padding:24px 28px;border-bottom:1px solid #efedf6;flex-shrink:0}.rules-header span{display:flex;gap:7px;align-items:center;font-size:12px;color:#9b81bc}.rules-header h2{font-size:23px;margin-top:8px}.rules-content{overflow-y:auto;overscroll-behavior:contain;padding:24px 28px;font-size:13px;line-height:1.9;min-height:0}.rules-content p{color:#8c809d;margin-top:8px}.rules-content h3{font-size:15px;color:#685780}.rules-overview{padding:18px;background:#f8f5fe;border-radius:12px}.rules-section{margin-top:24px}.rules-section article{margin-top:16px}.rules-section h4{margin:0;font-size:12px;font-weight:600;color:#8b749f}.rules-section article p{margin-top:4px}.rules-error{margin-top:18px;color:#c95868}.rules-error button{margin-left:12px}.rules-footer{padding:16px 28px;border-top:1px solid #efedf6;flex-shrink:0}.rules-footer button{width:100%;justify-content:center}
@media(max-width:640px){.rules-header{padding:20px}.rules-header h2{font-size:21px}.rules-content{padding:20px;font-size:12px}.rules-footer{padding:14px 20px}.rules-dialog{border-radius:16px}.view-rules{font-size:11px}}
</style>
