<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import { ChevronLeft, ChevronRight, Clock3, RefreshCw, ListOrdered, MousePointer2, Shuffle, Radio, AlertCircle } from 'lucide-vue-next'
import type { Game } from '../data'
import { getJSON, type Session, type State, type History, type PickSubmission } from '../api/games'
import DrawNumbers from './DrawNumbers.vue'
import MarketBoard from './MarketBoard.vue'
import GameRules from './GameRules.vue'
import StakeAmount from './StakeAmount.vue'
import {parseStake} from '../lib/stake'
import {type PlaySetting,key} from '../api/account'
const props = defineProps<{ game: Game; balance: number; playSettings: PlaySetting[]; submitting: boolean; receiptVersion: number; loggedIn: boolean }>()
const emit = defineEmits<{ login: []; back: []; records: []; notify: [message: string]; submit: [pick: PickSubmission] }>()
const sessions = ref<Session[]>([]), sessionID = ref(''), state = ref<State | null>(null), history = ref<History | null>(null)
const marketBusy=ref(false)
const loading = ref(true), error = ref(''), tab = ref<'history' | 'play' | 'classic'>('play'), page = ref(1), amount = ref('10'), picks = ref<number[]>([])
const requestKey=ref(key())
const playSetting=computed(()=>props.playSettings.find(p=>p.session_id===sessionID.value))
const stake=computed(()=>parseStake(amount.value))
const sealed=computed(()=>!!state.value && now.value+offset.value >= Date.parse(state.value.bet_close_at))
watch(()=>props.receiptVersion,()=>resetPicks())
watch(amount,()=>{if(!props.submitting)requestKey.value=key()})
const session = computed(() => sessions.value.find(s => s.id === sessionID.value))
const now = ref(Date.now()), offset = ref(0)
let inFlight: AbortController | null = null, poll: ReturnType<typeof setInterval> | undefined, tick: ReturnType<typeof setInterval> | undefined
let mounted = true
const padded = computed(() => ['pk10','lhc'].includes(props.game.id))
const unique = computed(() => ['ssc','pk10','lhc'].includes(props.game.id))
const lhc = computed(() => props.game.id === 'lhc')
const pickCount = computed(() => lhc.value ? 6 : props.game.rows)
const isComplete = computed(() => picks.value.length === pickCount.value && picks.value.every(n => n >= 0))
const choices = computed(() => Array.from({length: props.game.zero ? props.game.max + 1 : props.game.max}, (_, i) => i + (props.game.zero ? 0 : 1)))
const rows = computed(() => history.value?.items.slice((page.value - 1) * 20, page.value * 20) ?? [])
const pages = computed(() => Math.ceil((history.value?.items.length ?? 0) / 20))
const clock = computed(() => {
 if (!state.value) return '-- : -- : --'
 const seconds = Math.max(0, Math.ceil((Date.parse(state.value.next_draw_at) - now.value - offset.value) / 1000))
 return [Math.floor(seconds / 3600), Math.floor(seconds % 3600 / 60), seconds % 60].map(n => String(n).padStart(2,'0')).join(' : ')
})
const expired = computed(() => !!state.value && Date.parse(state.value.next_draw_at) <= now.value + offset.value)
const serverStale = computed(() => !!state.value && now.value + offset.value - Date.parse(state.value.server_time) > 10000)
const blocked = computed(() => props.submitting || !playSetting.value || playSetting.value.enabled===false || sealed.value || !!error.value || loading.value || expired.value || serverStale.value || !state.value || state.value.pending)
const datePart = (date: string, part: 'date' | 'time') => new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai', ...(part === 'date' ? {year:'numeric',month:'2-digit',day:'2-digit'} as const : {hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false} as const) }).format(new Date(date))
const formatNumber = (n: number) => n < 0 ? '—' : padded.value ? String(n).padStart(2,'0') : String(n)
function resetPicks() { requestKey.value=key(); picks.value = lhc.value ? [] : Array(props.game.rows).fill(-1); amount.value = '10' }
async function refresh(initial = false) {
 if (!sessionID.value) return
 if (inFlight) { if (!initial) return; inFlight.abort() }
 const controller = new AbortController(); inFlight = controller
 const selected = sessionID.value; if (initial) loading.value = true
 const timeout = setTimeout(() => controller.abort(), 8000)
 try {
  const started = Date.now()
  const [nextState, nextHistory] = await Promise.all([getJSON<State>(`/api/v1/sessions/${selected}/state`,controller.signal),getJSON<History>(`/api/v1/sessions/${selected}/draws?limit=100`,controller.signal)])
  if (!mounted || sessionID.value !== selected || controller.signal.aborted) return
  offset.value = Date.parse(nextState.server_time) - (started + Date.now()) / 2
  state.value = nextState; history.value = nextHistory; error.value = ''
  page.value = Math.min(page.value, Math.max(1,Math.ceil(nextHistory.items.length/20)))
 } catch (e) { if (mounted && sessionID.value === selected && inFlight === controller) error.value = e instanceof Error && e.name !== 'AbortError' ? e.message : '请求超时，请重试' }
 finally { clearTimeout(timeout); if (inFlight === controller) { inFlight = null; loading.value = false } }
}
async function initialize() {
 loading.value = true
 try { const data = await getJSON<{sessions:Session[]}>('/api/v1/games',AbortSignal.timeout(8000)); if (!mounted) return; sessions.value = data.sessions.filter(s => s.game_id === props.game.id); if (!sessions.value.length) throw new Error('该游戏暂无可用场次'); sessionID.value = sessions.value[0]!.id }
 catch(e) { if(mounted){error.value = e instanceof Error ? e.message : '连接失败';loading.value=false} }
}
watch(expired, value => { if(value) void refresh() })
watch(sessionID, () => {state.value = null;history.value = null;page.value = 1;resetPicks();void refresh(true)})
onMounted(() => {void initialize();poll = setInterval(() => void refresh(),1000);tick = setInterval(() => now.value = Date.now(),250)})
onUnmounted(() => {mounted=false;inFlight?.abort();clearInterval(poll);clearInterval(tick)})
function select(row: number,n: number) {
 if(props.submitting)return;requestKey.value=key()
 if (lhc.value) {if(picks.value.includes(n)) picks.value=picks.value.filter(v=>v!==n); else if(picks.value.length<6) picks.value=[...picks.value,n];else emit('notify','单式请选择 6 个不同号码');return}
 if(unique.value && picks.value.some((v,i)=>v===n && i!==row)){emit('notify','这个号码已在其他位置选择');return};picks.value[row]=picks.value[row]===n?-1:n
}
function randomize() {if(props.submitting)return;requestKey.value=key();const pool=[...choices.value];for(let i=pool.length-1;i>0;i--){const j=Math.floor(Math.random()*(i+1));[pool[i],pool[j]]=[pool[j]!,pool[i]!]};picks.value=unique.value?pool.slice(0,pickCount.value):Array.from({length:pickCount.value},()=>choices.value[Math.floor(Math.random()*choices.value.length)]!)}
function submit() {
 if(!isComplete.value)return emit('notify',lhc.value?'请选择 6 个不同号码':'请完成所有位置选号')
 if(blocked.value||!session.value||!state.value)return emit('notify','当前期号正在更新，请稍后重试')
 if(stake.value<(playSetting.value?.unit_cents??200)||stake.value>(playSetting.value?.max_stake_cents??100000))return emit('notify','请输入本场允许的投注金额')
 if(props.loggedIn&&props.balance*100<stake.value)return emit('notify','余额不足')
 const target = state.value.next_draw_at.slice(0,16).replaceAll('-','').replace('T','').replace(':','')
 emit('submit',{numbers:[...picks.value],unitCents:playSetting.value?.unit_cents??200,requestKey:requestKey.value,amountCents:stake.value,sessionID:session.value.id,sessionName:session.value.name,targetIssue:target})
}
function rowName(i: number) {if(props.game.id==='pk10')return `第${i+1}名`;if(props.game.id==='pc28')return '和值';if(props.game.rows===5)return ['万位','千位','百位','十位','个位'][i];return `第${i+1}位`}
</script>
<template>
 <div class="game-room">
  <button :disabled="marketBusy" class="back-link" @click="emit('back')"><ChevronLeft :size="17"/>返回游戏大厅</button>
  <div class="room-heading"><div><p class="section-eyebrow">STARLIGHT · LIVE DRAW</p><h1>{{ game.name }}<span class="room-type">{{ lhc ? '每日开奖' : '定时开奖' }}</span></h1><p>{{ lhc ? '六个正码，一个特别号。每晚 21:30，静候新一期。' : '每一次开奖，都有记录可循。' }}</p></div><button class="secondary-button" @click="emit('records')"><ListOrdered :size="16"/>我的订单记录</button></div>
  <div class="session-bar"><div class="session-options" role="tablist" aria-label="选择场次"><button v-for="s in sessions" :key="s.id" role="tab" :disabled="marketBusy" :aria-selected="sessionID === s.id" :class="{active:sessionID===s.id}" @click="sessionID=s.id"><Clock3 :size="16"/>{{ s.name }}<span v-if="sessionID===s.id" class="session-dot"></span></button></div><span class="beijing-time">北京时间 <b>{{ datePart(new Date(now+offset).toISOString(),'time') }}</b></span></div>
  <div v-if="error" class="room-error" role="alert"><AlertCircle :size="18"/><span>{{ error }}，暂不更新开奖结果。</span><button @click="sessionID ? refresh(true) : initialize()">重试</button></div>
  <div v-if="loading && !state" class="room-loading">正在同步场次与开奖记录…</div>
  <template v-if="state && session">
   <section class="live-draw-card"><div class="last-result"><div class="draw-caption"><span><Radio :size="14"/>最近一期</span><b>{{ state.latest?.issue ?? '等待开奖' }}</b><em v-if="state.latest?.origin==='simulated_history'" class="simulated_history">模拟历史</em></div><DrawNumbers v-if="state.latest" :round="state.latest" :format="session.format"/><p class="draw-timestamp" v-if="state.latest">开奖时间 {{ datePart(state.latest.draw_at,'date') }} {{ datePart(state.latest.draw_at,'time') }}</p></div><div class="next-result"><small>{{ expired ? '正在开奖 · 等待结果' : sealed ? '本期已封盘 · 距离开奖' : state.pending ? '等待本期结果 · 下一期倒计时' : '距离下期开奖' }}</small><strong>{{ clock }}</strong><span>{{ lhc?'每天 21:30:05':`每 ${session.interval_minutes} 分钟 · 第05秒开奖` }} <i>· 北京时间</i></span></div></section>
   <div class="draw-timing" :class="{sealed}"><span>{{sealed?'已封盘，等待开奖':'本期投注中'}}</span><span>截止 {{datePart(state.bet_close_at,'time')}}</span><span>开奖 {{datePart(state.next_draw_at,'time')}}</span><GameRules :key="sessionID" :game="game" :session="session"/></div>

   <div class="room-tabs" role="tablist" aria-label="游戏内容"><button role="tab" :aria-selected="tab==='history'" :class="{active:tab==='history'}" @click="tab='history'"><ListOrdered :size="18"/>开奖记录</button><button role="tab" :aria-selected="tab==='play'" :class="{active:tab==='play'}" @click="tab='play'"><MousePointer2 :size="17"/>两面 / 常规</button><button v-if="lhc" role="tab" :aria-selected="tab==='classic'" :class="{active:tab==='classic'}" @click="tab='classic'">六号单式</button><span v-if="tab==='history'" class="room-tab-meta">最近 {{history?.items.length??0}} 期</span></div>
   <section v-if="tab==='history'" class="draw-history"><div class="history-intro"><span><i></i>按期号倒序排列</span><button :disabled="loading" @click="refresh()" aria-label="刷新开奖记录"><RefreshCw :size="15"/>刷新</button></div><div class="history-table"><div class="history-head"><span>期号</span><span>开奖结果</span><span>开奖时间</span></div><div class="history-row" v-for="round in rows" :key="round.issue"><div class="history-issue"><strong>{{ round.issue }}</strong><small v-if="round.origin==='simulated_history'" class="simulated_history">模拟历史</small></div><DrawNumbers :round="round" :format="session.format" compact/><div class="history-time"><strong>{{ datePart(round.draw_at,'time') }}</strong><span>{{ datePart(round.draw_at,'date') }}</span></div></div><div v-if="!rows.length" class="room-loading">还没有开奖记录</div></div><div class="history-pagination"><span>共 {{ history?.total ?? 0 }} 期 · 每页 20 期</span><div><button :disabled="page<=1" aria-label="上一页" @click="page--"><ChevronLeft :size="16"/></button><button v-for="n in pages" :key="n" :aria-label="`第${n}页`" :class="{active:page===n}" @click="page=n">{{ n }}</button><button :disabled="page>=pages" aria-label="下一页" @click="page++"><ChevronRight :size="16"/></button></div></div></section>
   <MarketBoard v-else-if="tab==='play'" :key="sessionID" :session-id="sessionID" :play-setting="playSetting" :target-issue="state.next_draw_at.slice(0,16).replaceAll('-','').replace('T','').replace(':','')" :blocked="blocked" :block-label="sealed?'本期已封盘':expired?'正在开奖':'暂不可投注'" @notify="emit('notify',$event)" @login="emit('login')" @busy="marketBusy=$event"/>
   <template v-else>
    <section class="selection-panel"><div class="selection-heading"><div><h3>{{ lhc?'六合彩 · 单式选号':game.id==='pc28'?'PC蛋蛋 · 和值选号':'自选号码' }}</h3><p>{{ lhc?'从 01–49 中选择 6 个不同号码':game.id==='pc28'?'选择一个 0–27 的和值，三个开奖号码相加得出结果':unique?'每个位置选择一个号码，各位置不可重复':'每个位置选择一个号码，可重复' }}</p></div><button class="secondary-button" @click="randomize"><Shuffle :size="16"/>随机一注</button></div>
     <div v-if="lhc" class="marksix-picks"><button v-for="n in choices" :key="n" :class="{picked:picks.includes(n)}" :aria-label="`六合彩号码${n}`" :aria-pressed="picks.includes(n)" @click="select(0,n)">{{ formatNumber(n) }}</button></div>
     <template v-else><div class="number-row" v-for="row in game.rows" :key="row"><strong>{{ rowName(row-1) }}</strong><div class="number-options"><button v-for="n in choices" :key="n" :class="{picked:picks[row-1]===n}" :aria-label="`第${row}行号码${n}`" :aria-pressed="picks[row-1]===n" @click="select(row-1,n)">{{ formatNumber(n) }}</button></div></div></template>
     <div class="selection-summary"><div><small>{{ lhc?`已选 ${picks.length}/6 个号码`:'当前选择' }}</small><strong>{{ picks.length?picks.map(formatNumber).join(' · '):'—' }}</strong></div><button class="text-button" :disabled="submitting" @click="resetPicks">清空</button></div>

    </section>
    <StakeAmount v-model="amount" :min="playSetting?.unit_cents??200" :max="playSetting?.max_stake_cents??100000" :disabled="submitting"/><div class="order-bar"><div class="order-total">投注金额 <strong>{{isComplete?(stake/100).toFixed(2):'0.00'}}</strong> 元</div><button class="primary-button" :disabled="blocked" @click="submit">{{ submitting?'提交中…':!playSetting?.enabled?'玩法已暂停':sealed?'本期已封盘':blocked?'等待期号更新':!loggedIn?'登录后选号':'确认选号' }}<ChevronRight :size="16"/></button></div>
   </template>
  </template>
 </div>
</template>
