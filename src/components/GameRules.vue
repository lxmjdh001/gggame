<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { BookOpen, ChevronRight, X } from 'lucide-vue-next'
import type { Game } from '../data'
import { getJSON, type Session } from '../api/games'
import type { MarketBook } from '../api/markets'
import { sessionSchedule } from '../lib/sessionSchedule'
import { markSixBettingGroups, markSixBettingGroup } from '../lib/marksix'
const props = defineProps<{ game: Game; session: Session }>()
const dialog = ref<HTMLDialogElement | null>(null)
const book = ref<MarketBook | null>(null), loading = ref(false), error = ref('')
let controller: AbortController | null = null
let previousOverflow: string | null = null
const introduction = computed(() => ({
 ssc: '每期由 0–9 中的 5 个不重复号码组成，依次对应第一球至第五球。',
 ffc: '每期由 5 个 0–9 号码组成，号码可重复，依次对应第一球至第五球。',
 k3: '每期开出三个骰子，每个骰子为 1–6 点，点数可重复。玩法包括双面、三军、围骰、全骰和点数。',
 pk10: '每期将 01–10 排列为十个名次，号码不重复。冠军车号加亚军车号得到冠亚和，范围 3–19；冠亚和是计算所得的和值，不是额外车号。龙虎只比较第1名与第10名。',
 lhc: '每期从 01–49 中开出 7 个不同号码：前 6 位为平码（正码），最后第 7 位为特码，即 6＋1。',
 pc28: '每期开出 3 个 0–9 号码，号码可重复，三个号码相加得到 0–27 的和值。',
} as Record<string, string>)[props.game.id])
const sections = computed(() => {
 if (props.game.id === 'lhc') {
  return markSixBettingGroups.map(group => {
   const rules = new Map<string, Set<string>>()
   for (const market of book.value?.markets ?? []) {
    if (markSixBettingGroup(market) !== group.id || !market.rule) continue
    if (!rules.has(market.rule)) rules.set(market.rule, new Set())
    rules.get(market.rule)!.add(market.group)
   }
   return {title: group.label, items: [...rules].map(([text, names]) => ({text, groups: [...names].join('、')}))}
  }).filter(section => section.items.length)
 }

 if (props.game.id === 'k3') {
  return ['双面','三军','围骰','全骰','点数'].map(group => ({
   title: group,
   items: [...new Set((book.value?.markets ?? []).filter(market => market.group === group && market.rule).map(market => market.rule))].map(text => ({text, groups: ''})),
  })).filter(section => section.items.length)
 }
 return (['two_sides', 'regular'] as const).map(category => {
 const rules = new Map<string, Set<string>>()
 for (const market of book.value?.markets ?? []) {
  if (market.category !== category || !market.rule) continue
  if (!rules.has(market.rule)) rules.set(market.rule, new Set())
  rules.get(market.rule)!.add(market.group)
 }
 return { title: category === 'two_sides' ? '两面玩法' : '常规玩法', items: [...rules].map(([text, groups]) => ({text, groups: [...groups].join('、')})) }
 })
})
const lhcReferenceTables = computed(() => {
 if (props.game.id !== 'lhc' || !book.value) return []
 const context = book.value.marksix_context
 return [
  {title:'色波号码', rows:[['red','红波'],['blue','蓝波'],['green','绿波']].map(([color,label])=>`${label}：${context?.numbers.filter(item=>item.color===color).map(item=>String(item.number).padStart(2,'0')).join('、')??'资料同步中'}`)},
  {title:'本期生肖号码', rows:context?.zodiacs.map(item=>`${item.label}：${item.numbers.map(number=>String(number).padStart(2,'0')).join('、')}`)??[]},
 ]
})
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
   <header class="rules-header"><div><span><BookOpen :size="16"/>玩法简介</span><h2 id="rules-title">{{ ['lhc','k3'].includes(game.id) ? session.game_name : game.name }}</h2></div><button class="icon-button" aria-label="关闭玩法简介" autofocus @click="close"><X :size="21"/></button></header>
   <div class="rules-content">
    <section class="rules-overview"><h3>{{ session.name }}</h3><p>{{ introduction }}</p><template v-if="game.id==='k3'"><p>三个骰子的点数总和为 3–18：4–10 为小，11–17 为大；单双按总和奇偶判定。开出 111、222、333、444、555、666 任一围骰时，大、小、单、双及三军全部不中奖，不退本金。围骰仅所选豹子或全骰、对应总点数可中奖。点数只可选择总和 4–17，开出 3 或 18 不中奖；其余围骰总和照常判奖。</p><p>各项赔率以投注时确认值为准。可多选，每项独立成注，每注只派彩一次。</p></template><template v-else-if="game.id==='pk10'"><p>冠亚和大于 11 为大（12–19），小于或等于 11 为小（3–11）；单双按冠亚和的奇偶判定。冠亚和为 11 时是小、单，没有和局，不会因开出 11 而退回本金。</p><p>第1–10名的指定号码玩法：先选择名次，再选择 01–10 中的一个车号，只有该名次开出的车号与所选号码相同才中奖。</p></template><template v-else-if="game.id==='lhc'"><p>特码、正特码及正特1–6的大小、单双、合数大小、合数单双、尾数大小，开49退本；对应位置指定号码49及色波绿波仍正常判奖。大小以1–24为小、25–48为大，合数大小以数字之和1–6为小、7–12为大。独立的特码半波玩法（含合单/合双）遇特码49退本。</p><p>正码页的总和计算全部7个号码：总和≤174为小、≥175为大，单双按总和奇偶判定。特码页只保留号码、两面、合数、尾大小与色波。</p><p>生肖按当前投注期号所属农历年对应号码；49在一肖、正肖、尾数、连肖、连尾和特码生肖中正常计算。一肖、正特尾数出现一次或多次均只派彩一次。正肖只看前6个正码，按所选生肖出现次数派彩，最多5次。合肖选2–11个生肖合为1注，只看第7个特码，开49退本。</p><p>选数超过组合所需数量时，按生成的不同组合分别成注。全不中提供5–12不中，连肖中和连尾提供2–5连，连肖不中提供2–4连；连肖不中要求所选生肖均未出现在七球中。正特码与正特1–6独立定价；半波合单/合双按特码数字和的奇偶判定，开49退本。</p><p>所有派彩赔率均含本金，以投注时确认的赔率为准。含本年生肖、含0尾的组合如配置特殊赔率，按每组实际选值取价，不按整批统一取价；本年生肖由当期开奖期的农历年确定。</p></template><p v-else>两面及指定位置玩法按下方本站规则判奖。大小的和数仅使大小投注退回本金，单双独立按奇偶判定。</p><p>{{ sessionSchedule(session) }}</p></section>
    <p v-if="loading" role="status">正在加载玩法…</p>
    <div v-else-if="error" class="rules-error" role="alert">{{ error }}<button class="text-button" @click="load">重试</button></div>
    <template v-else><section v-for="section in sections" :key="section.title" class="rules-section"><h3>{{ section.title }}</h3><p v-if="section.title==='常规玩法' && !['pk10','k3','lhc'].includes(game.id)">先选择固定位置，再选择该位置的大小单双或指定数字；大小单双判定与两面玩法一致。</p><p v-if="section.title==='常规玩法' && game.id==='pk10'">指定冠亚和值可选择 3–19 中的一个数字，只有冠军车号与亚军车号相加等于所选和值才中奖；选择一组和值时，实际冠亚和命中所选组内任一和值即中奖。每注只派彩一次，各项赔率以投注时确认的赔率为准，可同时选择多项独立投注。</p><article v-for="item in section.items" :key="item.text"><h4 v-if="item.groups">{{ item.groups }}</h4><p>{{ item.text }}</p></article></section><section v-for="table in lhcReferenceTables" :key="table.title" class="rules-section"><h3>{{table.title}}</h3><p v-for="row in table.rows" :key="row">{{row}}</p></section></template>

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
