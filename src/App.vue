<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, nextTick } from 'vue'
import { House, Gift, Headphones, UserRound, ChevronRight, Bell, Wallet, FileText, X, Check, ShieldCheck, Smartphone, Clock3 } from 'lucide-vue-next'
import HomeLobby from './components/HomeLobby.vue'
import GameRoom from './components/GameRoom.vue'
import GameAccessGate from './components/GameAccessGate.vue'
import HelpCenter from './components/HelpCenter.vue'
import MarkSixLobby from './components/MarkSixLobby.vue'
import PromotionsPage from './components/PromotionsPage.vue'
import type { PickSubmission } from './api/games'
import { games, isGameAvailable, type Game } from './data'
import AuthDialog from './components/AuthDialog.vue'
import AccountPanel from './components/AccountPanel.vue'
import AdminPanel from './components/AdminPanel.vue'
import {user,site,balance,api,initializeAccount,refreshAccount,refreshSite,logout} from './api/account'
const ADMIN_PATH = '/f7cc4f635d178admin'
const page = ref(location.pathname === ADMIN_PATH ? 'admin' : 'home'), category = ref('all'), modal = ref(''), toast = ref(''), activeGame = ref<Game | null>(null)
const activeSessionId = ref(''), marketPending = ref(false), walletPending = ref(false)
const announcement = computed(()=>site.value?.announcement === '欢迎来到星彩娱乐。请留意各场次开奖时间，投注前查看玩法说明。' ? '欢迎来到金沙娱乐。请留意各场次开奖时间，投注前查看玩法说明。' : site.value?.announcement)
const walletMode=ref<'deposit'|'withdraw'>('deposit')
const authOpen=ref(!!new URLSearchParams(location.search).get('invite')),submitting=ref(false),receiptVersion=ref(0)
const favoriteIds = ref<string[]>([])
try { const saved = JSON.parse(localStorage.getItem('starlight-favorites') || '[]'); if (Array.isArray(saved)) favoriteIds.value = saved.filter((id: unknown) => typeof id === 'string') } catch {}
const nav = [{ id: 'home', label: '游戏大厅', icon: House }, { id: 'promotions', label: '福利活动', icon: Gift }, { id: 'records', label: '订单记录', icon: FileText }, { id: 'profile', label: '个人中心', icon: UserRound }]
let toastTimer: ReturnType<typeof setTimeout> | undefined
onUnmounted(() => { clearTimeout(toastTimer) })
function notify(message: string) { toast.value = message; clearTimeout(toastTimer); toastTimer = setTimeout(() => toast.value = '', 3200) }
function navigate(id: string) { if(marketPending.value){notify('投注结果待确认，请先重试确认原订单');return};if(walletPending.value){notify('资金申请结果待确认，请先确认原申请');return}; const path = id === 'admin' ? ADMIN_PATH : '/'; if(location.pathname !== path) history.replaceState(null, '', path); page.value = id; activeGame.value = null; activeSessionId.value = ''; window.scrollTo({ top: 0, behavior: 'smooth' }) }
function openSession(id: string) { const gameID = id.startsWith('lhc-') ? 'lhc' : id.startsWith('k3-') ? 'k3' : ''; const game = games.find(game => game.id === gameID); if (!game) return; openGame(game); activeSessionId.value = id }
function openGame(game: Game) { if(marketPending.value){notify('投注结果待确认，请先重试确认原订单');return};if(walletPending.value){notify('资金申请结果待确认，请先确认原申请');return};if(!isGameAvailable(game.id)){void openModal('coming-soon');return}; activeGame.value = game; activeSessionId.value = ''; window.scrollTo({ top: 0, behavior: 'smooth' }) }
function favorite(id: string) { favoriteIds.value = favoriteIds.value.includes(id) ? favoriteIds.value.filter(x => x !== id) : [...favoriteIds.value, id]; try { localStorage.setItem('starlight-favorites', JSON.stringify(favoriteIds.value)) } catch { notify('浏览器未允许保存收藏') } }
async function savePick(pick: PickSubmission) {
 if(!user.value){authOpen.value=true;return};if(submitting.value)return;submitting.value=true
 try {await api('/orders','POST',{session_id:pick.sessionID,issue:pick.targetIssue,numbers:pick.numbers,amount_cents:pick.amountCents,unit_cents:pick.unitCents,key:pick.requestKey});receiptVersion.value++;await refreshAccount();notify('订单已保存，余额已更新')}catch(e){notify((e as Error).message)}finally{submitting.value=false}
}
async function refreshBalance(){try{await refreshAccount();notify(user.value?'余额已更新':'请先登录')}catch(e){notify((e as Error).message)}}
async function signOut(){try{await logout();navigate('home');notify('已退出登录')}catch(e){notify((e as Error).message)}}
function signedIn(){authOpen.value=false;notify('登录成功');if(location.pathname===ADMIN_PATH)navigate('admin')}
let accountPoll:ReturnType<typeof setInterval>|undefined
onMounted(()=>{void initializeAccount().catch(e=>notify(e.message));accountPoll=setInterval(()=>{void refreshSite().catch(()=>{});void refreshAccount().catch(()=>{})},15000)})
onUnmounted(()=>clearInterval(accountPoll))
const dialogRef = ref<HTMLElement | null>(null)
let previousFocus: HTMLElement | null = null
async function openModal(name: string) { previousFocus = document.activeElement as HTMLElement; modal.value = name; await nextTick(); dialogRef.value?.focus() }
function closeModal() { modal.value = ''; previousFocus?.focus() }
function trapFocus(event: KeyboardEvent) { if (event.key !== 'Tab') return; const elements = dialogRef.value?.querySelectorAll<HTMLElement>('button, input, a[href], select, [tabindex="0"]'); if (!elements?.length) return; const first = elements[0], last = elements[elements.length - 1]; if (event.shiftKey && (document.activeElement === first || document.activeElement === dialogRef.value)) { event.preventDefault(); last?.focus() } else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() } }
</script>

<template>
  <div class="app-shell" :class="{'lobby-home':page==='home' && !activeGame,'profile-active':page==='profile','invite-active':page==='commissions','wallet-active':page==='wallet'&&!activeGame,'records-active':page==='records'&&!activeGame,'compact-game-active':activeGame && ['lhc','k3','pk10'].includes(activeGame.id) && (activeGame.id!=='lhc'||!!activeSessionId)}">
    <aside class="sidebar">
      <a href="#" class="brand" @click.prevent="navigate('home')"><img class="js-brand-logo" src="/js-logo.jpg" alt="JS 金沙"/></a>
      <div class="sidebar-caption">发现你的精彩</div>
      <nav class="side-nav"><button v-for="item in nav" :key="item.id" :class="{ active: page === item.id }" @click="navigate(item.id)"><component :is="item.icon" :size="20"/>{{ item.label }}<ChevronRight v-if="page === item.id" :size="15" class="nav-arrow"/></button></nav>
      <button class="side-service" :class="{active:page==='commissions'}" @click="navigate('commissions')"><Gift :size="20"/>邀请好友</button><button v-if="user?.role==='admin'" class="side-service" @click="navigate('admin')"><ShieldCheck :size="20"/>管理员后台</button><div class="sidebar-line"></div><div class="sidebar-caption">为你服务</div>
      <button class="side-service" @click="openModal('support')"><Headphones :size="20"/>帮助中心</button>
      <button class="side-service" @click="openModal('mobile')"><Smartphone :size="20"/>手机畅玩</button>
      <div class="sidebar-bottom"><span class="mini-stars">✦</span><strong>让每一刻，都有期待</strong><p>轻松探索，发现更多精彩</p><span class="service-label">账户与记录，随时查看</span></div>
      <div class="sidebar-footer"><ShieldCheck :size="14"/>账户服务 · 安全登录</div>
    </aside>

    <div class="main-shell">
      <header class="desktop-topbar"><div class="breadcrumb">探索 <ChevronRight :size="14"/><strong>{{ activeGame ? activeGame.name : nav.find(n => n.id === page)?.label || ({wallet:'我的钱包',commissions:'邀请好友',ledger:'账变记录',transactions:'交易记录',daily:'每日输赢',admin:'管理员后台'} as Record<string,string>)[page] }}</strong></div><div class="topbar-right"><span class="status"><i></i>欢迎来到金沙</span><button class="icon-button" aria-label="消息通知" @click="openModal('notifications')"><Bell :size="20"/><b class="notification-dot"></b></button><button class="avatar" @click="navigate('profile')">S</button><span v-if="user" class="user-name">{{user.username}}<small>{{user.role==='admin'?'ADMIN':'STAR MEMBER'}}</small></span><button v-else class="primary-button header-login" @click="authOpen=true">登录 / 注册</button></div></header>
      <main>
        <HomeLobby v-if="page === 'home' && !activeGame" :balance="balance" :username="user?.username" :announcement="announcement" :favorite-ids="favoriteIds" :category="category" @category="category=$event" @game="openGame" @session="openSession" @navigate="navigate" @wallet="mode=>{walletMode=mode;navigate('wallet')}" @help="openModal('support')" @notice="openModal('notifications')" @login="authOpen=true" @refresh="refreshBalance" @favorite="favorite" @notify="notify"/>

        <MarkSixLobby v-else-if="activeGame?.id==='lhc' && !activeSessionId" :game="activeGame" @back="activeGame=null" @select="activeSessionId=$event"/>
        <GameAccessGate v-else-if="activeGame" :key="activeGame.id" :pending="marketPending||submitting" @back="activeGame=null;activeSessionId=''">
         <GameRoom :initial-session-id="activeSessionId" :key="activeGame.id" :game="activeGame" :balance="balance" :play-settings="site?.plays ?? []" :submitting="submitting" :receipt-version="receiptVersion" :logged-in="!!user" @busy="marketPending=$event" @back="activeGame=null;activeSessionId=''" @records="navigate('records')" @notify="notify" @submit="savePick" @login="authOpen=true"/>
        </GameAccessGate>

        <AccountPanel v-else-if="['profile','wallet','records','commissions','ledger','transactions','daily'].includes(page)" :page="page" :wallet-mode="walletMode" @busy="walletPending=$event" @login="authOpen=true" @navigate="navigate" @notify="notify" @logout="signOut" @wallet="mode=>{walletMode=mode;navigate('wallet')}"/>
        <AdminPanel v-else-if="page==='admin'" @login="authOpen=true" @notify="notify"/>
        <PromotionsPage v-else-if="page==='promotions'" @navigate="action=>{if(action==='favorites'){navigate('home');category='favorites'}else navigate(action)}"/>

        <footer class="main-footer"><span>✦ JS CLUB</span><p>让每一刻，都有期待</p><small>账户中心 · 定时开奖</small></footer>
      </main>
    </div>
    <nav class="mobile-nav"><button v-for="item in [{id:'home',label:'首页',icon:House},{id:'promotions',label:'优惠',icon:Gift},{id:'wallet',label:'资金',icon:Wallet},{id:'profile',label:'我的',icon:UserRound}]" :key="item.id" :class="{ active: page === item.id }" @click="navigate(item.id)"><component :is="item.icon" :size="23"/><span>{{ item.id === 'home' ? '首页' : item.id === 'profile' ? '我的' : item.label }}</span><i v-if="page === item.id"></i></button></nav>
    <AuthDialog v-if="authOpen" @close="authOpen=false" @success="signedIn"/>
    <Transition name="toast"><div v-if="toast" class="toast-message" role="status"><Check :size="18"/>{{ toast }}</div></Transition>
    <div v-if="modal" class="modal-overlay" @click.self="closeModal" @keydown.esc="closeModal" @keydown="trapFocus">
      <section ref="dialogRef" class="modal" role="dialog" aria-modal="true" aria-labelledby="dialog-title" tabindex="-1">
        <button class="modal-close icon-button" aria-label="关闭弹窗" @click="closeModal"><X :size="22"/></button>
        <div class="modal-emblem"><component :is="modal === 'support' ? Headphones : modal === 'notifications' ? Bell : modal === 'mobile' ? Smartphone : modal === 'coming-soon' ? Clock3 : Wallet" :size="29"/></div>
        <h2 id="dialog-title">{{ ({support:'帮助中心', notifications:'金沙公告', wallet:'我的钱包', withdraw:'提现功能说明', mobile:'随时随地，畅享金沙', about:'关于金沙娱乐', 'coming-soon':'敬请期待'} as Record<string,string>)[modal] }}</h2>
        <template v-if="modal === 'coming-soon'"><p>游戏即将上线，敬请期待！</p></template>
        <template v-else-if="modal === 'support'"><HelpCenter/></template>
        <template v-else-if="modal === 'notifications'"><div class="notice-content"><span>站点公告</span><h3>全新金沙游戏大厅，与你见面</h3><p>{{announcement}}</p></div></template>
        <template v-else-if="modal === 'wallet' || modal === 'withdraw'"><p>可在钱包中查看余额、资金流水和申请进度。</p><div class="modal-balance"><small>可用余额</small><strong>{{ balance.toLocaleString() }}</strong></div><p>请在钱包页面提交充值或提现申请，由管理员核对处理。</p></template>
        <template v-else-if="modal === 'mobile'"><p>使用手机浏览器打开此站点，即可自动切换为手机布局。</p><p class="modal-note">可将本站加入浏览器书签，方便下次访问。请使用同一账户登录查看订单和余额。</p></template>
        <template v-else><p>金沙娱乐支持电脑和手机访问。</p><p>各场次按页面显示的时间开奖，历史结果可在开奖记录中查看。</p></template>
        <button class="primary-button modal-done" @click="closeModal">知道了</button>
      </section>
    </div>
  </div>
</template>

<style scoped>
.wallet-active .desktop-topbar,.wallet-active .main-footer,.wallet-active .mobile-nav{display:none}
@media(max-width:760px){.invite-active main{padding:0}.wallet-active .main-shell{padding-bottom:0}.wallet-active main{padding:0;min-height:100dvh}}
</style>

<style scoped>
.records-active .desktop-topbar,.records-active .main-footer,.records-active .mobile-nav{display:none}
@media(max-width:760px){.records-active .main-shell{padding-bottom:0}.records-active main{padding:0;min-height:100dvh}}
</style>
