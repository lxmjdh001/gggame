<script setup lang="ts">
import { formatIssue } from '../lib/issueDisplay'
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import { ChevronLeft, ChevronRight, Clock3, RefreshCw, ListOrdered, MousePointer2, Shuffle, Radio, AlertCircle, ArrowRightLeft, Menu } from 'lucide-vue-next'
import type { Game } from '../data'
import { getJSON, type Session, type State, type History, type PickSubmission } from '../api/games'
import DrawNumbers from './DrawNumbers.vue'
import RacingRound from './RacingRound.vue'
import MarkSixRound from './MarkSixRound.vue'
import DiceRound from './DiceRound.vue'
import MarketBoard from './MarketBoard.vue'
import GameRules from './GameRules.vue'
import StakeAmount from './StakeAmount.vue'
import {parseStake} from '../lib/stake'
import {formatDrawTime} from '../lib/drawTime'
import {roundPhase,roundStatusLabels} from '../lib/roundStatus'
import {sessionPeriod,sessionSchedule} from '../lib/sessionSchedule'
import {type PlaySetting,key,refreshAccount} from '../api/account'
const props = defineProps<{ game: Game; initialSessionId?: string; balance: number; playSettings: PlaySetting[]; submitting: boolean; receiptVersion: number; loggedIn: boolean }>()
const emit = defineEmits<{ login: []; back: []; records: []; notify: [message: string]; submit: [pick: PickSubmission]; busy: [busy: boolean] }>()
const sessions = ref<Session[]>([]), sessionID = ref(''), state = ref<State | null>(null), history = ref<History | null>(null)
const marketBusy=ref(false), sessionMenu=ref(false), roomMenu=ref(false)
watch(marketBusy,value=>emit('busy',value),{flush:'sync'})
const racingGame=computed(()=>props.game.id==='pk10')
const diceGame=computed(()=>props.game.id==='k3')
const refreshingBalance=ref(false)
const compactGame=computed(()=>['lhc','k3','pk10'].includes(props.game.id))
const loading = ref(true), error = ref(''), tab = ref<'history' | 'play' | 'classic'>('play'), page = ref(1), amount = ref('10'), picks = ref<number[]>([])
const requestKey=ref(key())
const playSetting=computed(()=>props.playSettings.find(p=>p.session_id===sessionID.value))
const stake=computed(()=>parseStake(amount.value))
watch(()=>props.receiptVersion,()=>resetPicks())
watch(amount,()=>{if(!props.submitting)requestKey.value=key()})
const session = computed(() => sessions.value.find(s => s.id === sessionID.value))
const hideSessionSelector = computed(() => compactGame.value || ['ssc','pk10'].includes(props.game.id) && sessions.value.length <= 1)
const now = ref(Date.now()), offset = ref(0)
let inFlight: AbortController | null = null, poll: ReturnType<typeof setInterval> | undefined, tick: ReturnType<typeof setInterval> | undefined
let mounted = true
const padded = computed(() => ['pk10','lhc'].includes(props.game.id))
const unique = computed(() => ['ssc','pk10','lhc'].includes(props.game.id))
const lhc = computed(() => props.game.id === 'lhc')
const maxNumber = computed(() => props.game.max)
const pickCount = computed(() => lhc.value ? 6 : props.game.rows)
const isComplete = computed(() => picks.value.length === pickCount.value && picks.value.every(n => n >= 0))
const choices = computed(() => Array.from({length: props.game.zero ? maxNumber.value + 1 : maxNumber.value}, (_, i) => i + (props.game.zero ? 0 : 1)))
const rows = computed(() => history.value?.items.slice((page.value - 1) * 20, page.value * 20) ?? [])
const pages = computed(() => Math.ceil((history.value?.items.length ?? 0) / 20))
const clock = computed(() => {
 if (!state.value) return '-- : -- : --'
 const seconds = Math.max(0, Math.ceil((Date.parse(state.value.next_draw_at) - now.value - offset.value) / 1000))
 return [Math.floor(seconds / 3600), Math.floor(seconds % 3600 / 60), seconds % 60].map(n => String(n).padStart(2,'0')).join(' : ')
})
const expired = computed(() => !!state.value && Date.parse(state.value.next_draw_at) <= now.value + offset.value)
const serverStale = computed(() => !!state.value && now.value + offset.value - Date.parse(state.value.server_time) > 10000)
const phase = computed(() => roundPhase(state.value,now.value+offset.value,playSetting.value?.enabled,!!error.value||serverStale.value||loading.value))
const bettingStatus = computed(() => roundStatusLabels[phase.value])
const blocked = computed(() => props.submitting || phase.value!=='open')
const datePart = (date: string, part: 'date' | 'time') => new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai', ...(part === 'date' ? {year:'numeric',month:'2-digit',day:'2-digit'} as const : {hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false} as const) }).format(new Date(date))
const formatNumber = (n: number) => n < 0 ? '—' : padded.value ? String(n).padStart(2,'0') : String(n)
function resetPicks() { requestKey.value=key(); picks.value = lhc.value ? [] : Array(props.game.rows).fill(-1); amount.value = '10' }
async function refreshDiceHeader() {
 if(refreshingBalance.value)return
 refreshingBalance.value=true
 try { await Promise.all([refresh(true),refreshAccount()]) }
 catch(e) { emit('notify',e instanceof Error?e.message:'刷新失败，请重试') }
 finally { refreshingBalance.value=false }
}
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
 try { const data = await getJSON<{sessions:Session[]}>('/api/v1/games',AbortSignal.timeout(8000)); if (!mounted) return; sessions.value = data.sessions.filter(s => s.game_id === props.game.id); if (!sessions.value.length) throw new Error('该游戏暂无可用场次'); sessionID.value = sessions.value.find(s => s.id === props.initialSessionId)?.id ?? sessions.value[0]!.id }
 catch(e) { if(mounted){error.value = e instanceof Error ? e.message : '连接失败';loading.value=false} }
}
watch(expired, value => { if(value) void refresh() })
watch(sessionID, () => {sessionMenu.value=false;roomMenu.value=false;state.value = null;history.value = null;page.value = 1;resetPicks();void refresh(true)})
onMounted(() => {void initialize();poll = setInterval(() => void refresh(),1000);tick = setInterval(() => now.value = Date.now(),250)})
onUnmounted(() => {mounted=false;inFlight?.abort();clearInterval(poll);clearInterval(tick);emit('busy',false)})
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
 <div class="game-room" :class="{'marksix-room':lhc,'reference-game':compactGame,'racing-room':racingGame,'racing-play-room':racingGame&&tab==='play','marksix-play-room':lhc&&tab==='play','dice-room':diceGame}">
  <div v-if="compactGame" class="compact-room-top">
  <header v-if="racingGame" class="racing-room-header"><button :disabled="marketBusy||submitting" aria-label="返回游戏大厅" @click="emit('back')"><ChevronLeft :size="22"/></button><h1>{{game.name}}</h1><button class="room-menu-toggle" :disabled="marketBusy||submitting" :aria-expanded="roomMenu" aria-controls="game-room-menu" :aria-label="`打开${game.name}菜单`" @click="roomMenu=!roomMenu;sessionMenu=false"><Menu :size="24"/></button></header>
  <header v-else-if="compactGame" class="reference-room-header"><button :disabled="marketBusy||submitting" aria-label="返回游戏大厅" @click="emit('back')"><ChevronLeft v-if="!lhc" :size="18"/>返回</button><button class="reference-game-name" :disabled="marketBusy||submitting" :aria-expanded="sessionMenu" aria-label="切换游戏场次" @click="sessionMenu=!sessionMenu;roomMenu=false"><ArrowRightLeft v-if="lhc" :size="22"/>{{session?.game_name||game.name}}<ChevronRight v-if="!lhc" :size="14"/></button><span>余额: {{balance.toFixed(2)}}</span><button v-if="diceGame||lhc" class="dice-header-refresh" :disabled="marketBusy||submitting||refreshingBalance" aria-label="刷新余额与开奖" @click="refreshDiceHeader"><RefreshCw :size="15" :class="{refreshing:refreshingBalance}"/></button><button class="marksix-menu-toggle room-menu-toggle" :disabled="marketBusy||submitting" :aria-expanded="roomMenu" aria-controls="game-room-menu" :aria-label="`打开${game.name}菜单`" @click="roomMenu=!roomMenu;sessionMenu=false"><Menu :size="24"/></button></header>
  <div v-if="roomMenu" id="game-room-menu" class="marksix-room-menu" :aria-label="`${game.name}菜单`" @keydown.esc.stop="roomMenu=false"><button :disabled="marketBusy||submitting" @click="tab='play';roomMenu=false">投注选号</button><button :disabled="marketBusy||submitting" @click="tab='history';roomMenu=false">开奖记录</button><button :disabled="marketBusy||submitting" @click="emit('records');roomMenu=false">投注记录</button><GameRules v-if="session" :key="sessionID" :game="game" :session="session"/></div>
  </div>
  <p v-if="lhc && now+offset&lt;Date.parse('2026-10-15T12:00:05+08:00')" class="reference-transition">北京时间10月15日12:00起按新周期开奖；切换前本期封盘与开奖状态以实时倒计时为准。</p>
  <div v-if="compactGame&&sessionMenu" class="reference-session-menu" aria-label="切换场次"><button v-for="s in sessions" :key="s.id" :disabled="marketBusy||submitting" :class="{active:sessionID===s.id}" @click="sessionID=s.id;sessionMenu=false">{{s.game_name}}</button></div>
  <button v-if="!compactGame" :disabled="marketBusy || submitting" class="back-link" @click="emit('back')"><ChevronLeft :size="17"/>{{lhc?'返回六合彩大厅':'返回游戏大厅'}}</button>
  <div v-if="!compactGame" class="room-heading"><div><p class="section-eyebrow">STARLIGHT · LIVE DRAW</p><h1>{{ lhc ? session?.game_name || game.name : game.name }}<span class="room-type">{{ sessionPeriod(session) }}</span></h1><p>{{ lhc && session ? `六个平码，一个特码。${sessionSchedule(session)}` : '每一次开奖，都有记录可循。' }}</p></div><button class="secondary-button" @click="emit('records')"><ListOrdered :size="16"/>我的订单记录</button></div>
  <div v-if="!compactGame" class="session-bar" :class="{'without-selector':hideSessionSelector}"><div v-if="!hideSessionSelector" class="session-options" role="tablist" aria-label="选择场次"><button v-for="s in sessions" :key="s.id" role="tab" :disabled="marketBusy || submitting" :aria-selected="sessionID === s.id" :class="{active:sessionID===s.id}" @click="sessionID=s.id"><Clock3 :size="16"/>{{ lhc ? s.game_name : s.name }}<span v-if="sessionID===s.id" class="session-dot"></span></button></div><span class="beijing-time">北京时间 <b>{{ datePart(new Date(now+offset).toISOString(),'time') }}</b></span></div>
  <div v-if="error" class="room-error" role="alert"><AlertCircle :size="18"/><span>{{ error }}，暂不更新开奖结果。</span><button @click="sessionID ? refresh(true) : initialize()">重试</button></div>
  <div v-if="loading && !state" class="room-loading">正在同步场次与开奖记录…</div>
  <template v-if="state && session">
   <RacingRound v-if="racingGame" :state="state" :phase="phase" :now="now+offset" :busy="marketBusy||submitting" :history-open="tab==='history'" @history="tab=tab==='history'?'play':'history'"/>
   <MarkSixRound v-else-if="lhc" :refreshing="refreshingBalance" @refresh="refreshDiceHeader" :state="state" :phase="phase" :now="now+offset" :busy="marketBusy||submitting" :history-open="tab==='history'" @history="tab=tab==='history'?'play':'history'"/>
   <DiceRound v-else-if="diceGame" :state="state" :phase="phase" :now="now+offset" :busy="marketBusy||submitting" :history-open="tab==='history'" @history="tab=tab==='history'?'play':'history'"/>
   <section v-else-if="compactGame" class="reference-round"><div class="reference-countdown"><Clock3 :size="18"/><span>{{formatIssue(state.next_draw_at.slice(0,16).replaceAll('-','').replace('T','').replace(':',''))}}期</span><small>{{bettingStatus}}</small><b>{{clock}}</b></div><button :disabled="marketBusy||submitting" class="reference-last" :aria-expanded="tab==='history'" @click="tab=tab==='history'?'play':'history'"><span>{{state.latest?formatIssue(state.latest.issue):'等待开奖'}}</span><DrawNumbers v-if="state.latest" :round="state.latest" :format="session.format" compact/><ChevronRight :size="15"/></button></section>
   <section v-else class="live-draw-card"><div class="last-result"><div class="draw-caption"><span><Radio :size="14"/>最近一期</span><b>{{ state.latest ? formatIssue(state.latest.issue) : '等待开奖' }}</b></div><DrawNumbers v-if="state.latest" :round="state.latest" :format="session.format"/><p class="draw-timestamp" v-if="state.latest">开奖时间 {{ datePart(state.latest.draw_at,'date') }} {{ formatDrawTime(state.latest.draw_at,'time') }}</p></div><div class="next-result"><small>{{ phase==='drawing' ? '开奖中，请稍候' : '距离下期开奖' }}</small><strong>{{ clock }}</strong><div class="betting-status" :class="{sealed:phase==='sealed'||phase==='drawing',unavailable:phase==='syncing'||phase==='paused'}" role="status">{{bettingStatus}}</div></div></section>

   <div v-if="!compactGame" class="room-tabs" role="tablist" aria-label="游戏内容"><button :disabled="marketBusy||submitting" role="tab" :aria-selected="tab==='history'" :class="{active:tab==='history'}" @click="tab='history'"><ListOrdered :size="18"/>开奖记录</button><button :disabled="marketBusy||submitting" role="tab" :aria-selected="tab==='play'" :class="{active:tab==='play'}" @click="tab='play'"><MousePointer2 :size="17"/>{{racingGame||game.id==='k3'||lhc?'投注选号':'两面 / 常规'}}</button><span v-if="tab==='history'" class="room-tab-meta">最近 {{history?.items.length??0}} 期</span><span class="room-rules"><GameRules :key="sessionID" :game="game" :session="session"/></span></div>
   <section v-if="tab==='history'" class="draw-history"><div class="history-intro"><span><i></i>按期号倒序排列</span><button :disabled="loading" @click="refresh()" aria-label="刷新开奖记录"><RefreshCw :size="15"/>刷新</button></div><div class="history-table"><div class="history-head"><span>{{diceGame?'期数 / 时间':'期号'}}</span><span>开奖结果</span><span v-if="!diceGame">开奖时间</span></div><div class="history-row" v-for="round in rows" :key="round.issue"><div class="history-issue"><strong :title="round.issue">{{ formatIssue(round.issue) }}</strong></div><DrawNumbers :round="round" :format="session.format" compact/><div class="history-time"><strong>{{ formatDrawTime(round.draw_at,'time') }}</strong><span>{{ datePart(round.draw_at,'date') }}</span></div></div><div v-if="!rows.length" class="room-loading">还没有开奖记录</div></div><div class="history-pagination"><span>共 {{ history?.total ?? 0 }} 期 · 每页 20 期</span><div><button :disabled="page<=1" aria-label="上一页" @click="page--"><ChevronLeft :size="16"/></button><button v-for="n in pages" :key="n" :aria-label="`第${n}页`" :class="{active:page===n}" @click="page=n">{{ n }}</button><button :disabled="page>=pages" aria-label="下一页" @click="page++"><ChevronRight :size="16"/></button></div></div></section>
   <MarketBoard v-else-if="tab==='play'" :initial-amount="lhc?'':undefined" :compact="compactGame" :racing="racingGame" :dice="diceGame" :key="sessionID" :session-id="sessionID" :play-setting="playSetting" :target-issue="state.next_draw_at.slice(0,16).replaceAll('-','').replace('T','').replace(':','')" :blocked="blocked" :block-label="submitting?'提交中…':bettingStatus" @notify="emit('notify',$event)" @login="emit('login')" @busy="marketBusy=$event"/>
   <template v-else>
    <section class="selection-panel"><div class="selection-heading"><div><h3>{{ lhc?'六合彩 · 单式选号':game.id==='pc28'?'PC蛋蛋 · 和值选号':'自选号码' }}</h3><p>{{ lhc?`从 01–${maxNumber} 中选择 6 个不同号码`:game.id==='pc28'?'选择一个 0–27 的和值，三个开奖号码相加得出结果':unique?'每个位置选择一个号码，各位置不可重复':'每个位置选择一个号码，可重复' }}</p></div><button class="secondary-button" @click="randomize"><Shuffle :size="16"/>随机一注</button></div>
     <div v-if="lhc" class="marksix-picks"><button v-for="n in choices" :key="n" :class="{picked:picks.includes(n)}" :aria-label="`六合彩号码${n}`" :aria-pressed="picks.includes(n)" @click="select(0,n)">{{ formatNumber(n) }}</button></div>
     <template v-else><div class="number-row" v-for="row in game.rows" :key="row"><strong>{{ rowName(row-1) }}</strong><div class="number-options"><button v-for="n in choices" :key="n" :class="{picked:picks[row-1]===n}" :aria-label="`第${row}行号码${n}`" :aria-pressed="picks[row-1]===n" @click="select(row-1,n)">{{ formatNumber(n) }}</button></div></div></template>
     <div class="selection-summary"><div><small>{{ lhc?`已选 ${picks.length}/6 个号码`:'当前选择' }}</small><strong>{{ picks.length?picks.map(formatNumber).join(' · '):'—' }}</strong></div><button class="text-button" :disabled="submitting" @click="resetPicks">清空</button></div>

    </section>
    <StakeAmount v-model="amount" :min="playSetting?.unit_cents??200" :max="playSetting?.max_stake_cents??100000" :disabled="submitting"/><div class="order-bar"><div class="order-total">投注金额 <strong>{{isComplete?(stake/100).toFixed(2):'0.00'}}</strong> 元</div><button class="primary-button" :disabled="blocked" @click="submit">{{ submitting?'提交中…':blocked?bettingStatus:!loggedIn?'登录后选号':'确认选号' }}<ChevronRight :size="16"/></button></div>
   </template>
  </template>
 </div>
</template>

<style scoped>
.session-bar.without-selector{justify-content:flex-end;margin-top:-10px;margin-bottom:12px}
.without-selector .beijing-time{margin-top:0}
.dice-room{background:#fff}.dice-room .reference-game-name{white-space:nowrap}.dice-room .dice-header-refresh{padding:7px;flex-shrink:0}.dice-header-refresh .refreshing{animation:dice-refresh-spin 1s linear infinite}@keyframes dice-refresh-spin{to{transform:rotate(360deg)}}
@media(prefers-reduced-motion:reduce){.dice-header-refresh .refreshing{animation:none}}
@media(max-width:360px){.dice-room .reference-room-header{gap:5px;padding:8px 6px}.dice-room .reference-room-header>.reference-game-name{font-size:13px}.dice-room .reference-room-header>span{font-size:9px}}

@media(max-width:760px){.marksix-room .session-options{display:grid;grid-template-columns:repeat(2,minmax(0,1fr))}.marksix-room .session-options button{min-width:0;white-space:nowrap}}
</style>

<style scoped>
.reference-transition{background:#fff7e8;color:#a77d43;padding:7px 10px;font-size:11px;line-height:1.5}.reference-room-header{display:flex;align-items:center;gap:10px;min-height:52px;padding:8px 14px;background:linear-gradient(110deg,#4f53ec,#9654ee);color:white}
.reference-room-header>button{display:flex;align-items:center;gap:4px;font-size:14px;padding:4px 0}.reference-room-header>.reference-game-name{font-size:17px;font-weight:600;min-width:0}.reference-game-name small{font-size:11px;font-weight:400}.reference-room-header>span{margin-left:auto;font-size:12px;font-variant-numeric:tabular-nums;white-space:nowrap}.reference-session-menu{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));background:#f3efff;gap:1px;padding:5px}.reference-session-menu button{background:white;padding:12px 3px;font-size:12px}.reference-session-menu button.active{background:#e2d6ff;color:#6a4eb5}.reference-round{background:#fff}.reference-countdown{display:flex;align-items:center;gap:8px;padding:8px 12px;border-bottom:1px solid #eee;color:#514c5a;font-size:12px;flex-wrap:wrap}.reference-countdown svg{color:#979a9e}.reference-countdown small{margin-left:auto;font-size:11px;color:#8b7d96}.reference-countdown b{color:#b57680;font-weight:500;font-variant-numeric:tabular-nums;white-space:nowrap}.reference-last{display:flex;align-items:center;gap:12px;padding:10px 12px;width:100%;text-align:left;border-bottom:1px solid #e5e5e5}.reference-last>span{font-size:11px;color:#57535d;white-space:nowrap}.reference-last>svg{margin-left:auto}.reference-game .room-tabs{margin:0;background:#fff;gap:17px;padding:5px 12px;border-bottom:1px solid #e4e2ea}.reference-game .room-tabs>button{font-size:11px;padding:8px 0}.reference-game .room-tabs>button svg{display:none}.reference-game .room-rules :deep(button){font-size:11px;padding:0}.reference-game :deep(.result-numbers){gap:5px;flex-wrap:nowrap}.reference-game :deep(.result-number){width:27px;height:27px;font-size:12px;box-shadow:inset 0 0 0 3px #fff4;margin:0}.reference-game :deep(.result-number small){display:none}.reference-game :deep(.math-sign){font-size:11px;margin:0}.reference-game :deep(.triple-sum){font-size:11px}.reference-game .draw-history{border-radius:0}
@media(max-width:760px){.reference-room-header{padding:8px;gap:6px;min-height:48px}.reference-room-header>button{font-size:12px}.reference-room-header>.reference-game-name{font-size:14px;gap:2px}.reference-game-name small{font-size:9px}.reference-room-header>span{font-size:10px}.reference-countdown{padding:7px 6px;gap:5px;font-size:10px}.reference-countdown small{font-size:9px}.reference-countdown svg{width:15px}.reference-last{padding:8px 6px;gap:6px;flex-wrap:wrap}.reference-last>span{font-size:9px}.reference-game :deep(.result-numbers){gap:3px}.reference-game :deep(.result-number){width:23px;height:23px;font-size:11px}.reference-game .room-tabs{gap:12px;padding:3px 8px}.reference-game .room-tabs>button{font-size:10px}.reference-game .room-rules :deep(button){font-size:10px}.reference-game .room-tabs .room-tab-meta{display:none}.reference-game .draw-history{margin:0 0 80px}.reference-session-menu{grid-template-columns:repeat(2,minmax(0,1fr))}}
</style>

<style scoped>
.racing-room{background:#fff}.racing-room-header{display:grid;grid-template-columns:80px minmax(0,1fr) 80px;align-items:center;min-height:50px;padding:0 12px;background:linear-gradient(110deg,#9661ff,#d264db 70%,#ef76ca);color:white}.racing-room-header h1{font-size:19px;font-weight:600;text-align:center;margin:0}.racing-room-header button{display:flex;align-items:center;font-size:12px;padding:10px 0}.racing-room-header button:last-child{justify-content:flex-end}.racing-room .room-tabs{padding:0 14px;gap:22px}.racing-room .room-tabs>button{font-size:11px;padding:9px 0}.racing-room .room-rules :deep(button){color:#8c70b5}.racing-room .draw-history{margin-bottom:0}@media(max-width:760px){.racing-room-header{padding:0 8px;min-height:44px}.racing-room-header h1{font-size:16px}.racing-room-header button{font-size:11px}.racing-room .room-tabs{gap:18px;padding:0 10px}.racing-room .room-tabs>button{font-size:10px;padding:7px 0}}
</style>

<style scoped>
/* Keep results and submission visible while the betting columns scroll. */
.racing-play-room,.marksix-play-room{display:flex;flex-direction:column;height:calc(100dvh - 30px);min-height:0;overflow:hidden}
.racing-play-room>*,.marksix-play-room>*{flex-shrink:0}
.racing-play-room>.racing-market,.marksix-play-room>.marksix-market{flex:1;min-height:0}
@media(min-width:1500px){.racing-play-room,.marksix-play-room{height:calc(100dvh - 36px)}}
@media(max-width:1200px){.racing-play-room,.marksix-play-room{height:calc(100dvh - 24px)}}
@media(max-width:1000px) and (min-width:761px){.racing-play-room,.marksix-play-room{height:calc(100dvh - 22px)}}
@media(max-width:760px){.racing-play-room,.marksix-play-room{height:100dvh}}
</style>

<style scoped>
.dice-room .draw-history{margin-top:0;border-top:0}.dice-room .history-head,.dice-room .history-row{grid-template-columns:150px minmax(0,1fr);column-gap:20px}.dice-room .history-head>span:last-child{text-align:left}.dice-room .history-row{min-height:64px;row-gap:4px;padding-top:10px;padding-bottom:10px}.dice-room .history-issue{grid-column:1;grid-row:1;align-self:end}.dice-room .history-issue strong{color:#625c69;letter-spacing:0}.dice-room .history-time{grid-column:1;grid-row:2;align-self:start;text-align:left}.dice-room .history-time strong{letter-spacing:0;font-weight:400}.dice-room .history-time>span{display:none}.dice-room .history-row>.result-numbers{grid-column:2;grid-row:1 / 3;justify-content:flex-start}
@media(max-width:760px){.dice-room .history-head{display:grid;height:37px;font-size:10px;padding:0 10px}.dice-room .history-head,.dice-room .history-row{grid-template-columns:110px minmax(0,1fr);column-gap:10px}.dice-room .history-row{min-height:58px;padding:8px 10px;row-gap:3px}.dice-room .history-issue strong{font-size:10px}.dice-room .history-time strong{font-size:11px}.dice-room .history-intro{padding:10px}}
</style>

<style scoped>
.marksix-room{--lhc-unit:min(1px,calc(100vw / 587));position:relative;background:#fff;font-family:Arial,'Microsoft YaHei','PingFang SC',sans-serif}
.marksix-room .reference-room-header{min-height:calc(78 * var(--lhc-unit));height:calc(78 * var(--lhc-unit));gap:calc(10 * var(--lhc-unit));padding:0 calc(22 * var(--lhc-unit));background:linear-gradient(105deg,#6555f5 0%,#8652ed 85%)}
.marksix-room .reference-room-header>button{padding:0;font-size:calc(24 * var(--lhc-unit));line-height:1.2;white-space:nowrap}
.marksix-room .reference-room-header>.reference-game-name{gap:calc(10 * var(--lhc-unit));font-size:calc(24 * var(--lhc-unit));font-weight:600}
.marksix-room .reference-game-name>svg{width:calc(26 * var(--lhc-unit));height:calc(26 * var(--lhc-unit))}
.marksix-room .reference-room-header>span{font-size:calc(20 * var(--lhc-unit));font-weight:600;line-height:1}
.marksix-room .dice-header-refresh>svg{width:calc(20 * var(--lhc-unit));height:calc(20 * var(--lhc-unit))}
.marksix-room .marksix-menu-toggle{margin-left:calc(12 * var(--lhc-unit))}
.marksix-room .marksix-menu-toggle>svg{width:calc(29 * var(--lhc-unit));height:calc(29 * var(--lhc-unit))}
.marksix-room-menu{position:absolute;right:0;top:calc(78 * var(--lhc-unit));z-index:12;display:flex;flex-direction:column;min-width:150px;padding:6px;background:#fff;border:1px solid #e8e3f3;box-shadow:0 5px 15px #40306426}
.marksix-room-menu>button,.marksix-room-menu :deep(.view-rules){justify-content:flex-start;width:100%;padding:13px 16px;font-size:14px;color:#51456a;text-align:left}
.marksix-room .reference-session-menu{position:absolute;left:0;right:0;top:calc(78 * var(--lhc-unit));z-index:11;box-shadow:0 4px 8px #0002}
@media(max-width:360px){.marksix-room .reference-room-header{padding:0 10px;gap:5px}.marksix-room .reference-room-header>span{font-size:10px}.marksix-room .marksix-menu-toggle{margin-left:3px}}
</style>

<style scoped>
.dice-room{font-family:Arial,'Microsoft YaHei','PingFang SC',sans-serif}
.dice-room .reference-room-header>button{font-size:16px}.dice-room .reference-room-header>.reference-game-name{font-size:19px}.dice-room .reference-room-header>span{font-size:13px}
.dice-room .room-tabs>button,.dice-room .room-rules :deep(button){font-size:14px}
@media(max-width:760px){.dice-room .reference-room-header>button{font-size:15px}.dice-room .reference-room-header>.reference-game-name{font-size:17px}.dice-room .reference-room-header>span{font-size:12px}.dice-room .room-tabs>button,.dice-room .room-rules :deep(button){font-size:13px}}
@media(max-width:360px){.dice-room .reference-room-header>button{font-size:14px}.dice-room .reference-room-header>.reference-game-name{font-size:15px}.dice-room .reference-room-header>span{font-size:11px}.dice-room .room-tabs>button,.dice-room .room-rules :deep(button){font-size:12px}}
</style>

<style scoped>
.game-room .history-issue strong{font-size:17px;letter-spacing:.2px}
@media(max-width:760px){.game-room .history-issue strong{font-size:15px}}
@media(max-width:360px){.game-room .history-issue strong{font-size:14px}}
</style>

<style scoped>
.compact-room-top{position:relative;z-index:15;flex-shrink:0}
.compact-room-top .marksix-room-menu{top:100%;min-width:160px}
.reference-room-header>.room-menu-toggle,.racing-room-header>.room-menu-toggle{flex-shrink:0;justify-content:center;width:30px;min-height:32px;padding:0}
.racing-room-header>.room-menu-toggle{justify-self:end}
.room-menu-toggle:focus-visible{outline:2px solid #fff;outline-offset:2px;border-radius:3px}
</style>
