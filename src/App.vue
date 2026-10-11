<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, nextTick } from 'vue'
import { Sparkles, House, Gift, Headphones, UserRound, ChevronRight, Search, Bell, Wallet, ArrowUpRight, FileText, Flame, LayoutGrid, Heart, X, ChevronLeft, Shuffle, Check, ShieldCheck, Clock3, Volume2, RefreshCw, Plus, Minus, BadgeCheck, CircleHelp, Smartphone } from 'lucide-vue-next'
import GameArtwork from './components/GameArtwork.vue'
import GameRoom from './components/GameRoom.vue'
import PromotionsPage from './components/PromotionsPage.vue'
import type { PickSubmission } from './api/games'
import { games, type Game } from './data'
import AuthDialog from './components/AuthDialog.vue'
import AccountPanel from './components/AccountPanel.vue'
import AdminPanel from './components/AdminPanel.vue'
import {user,account,site,balance,api,initializeAccount,refreshAccount,refreshSite,logout,key} from './api/account'
const ADMIN_PATH = '/f7cc4f635d178admin'
const page = ref(location.pathname === ADMIN_PATH ? 'admin' : 'home'), category = ref('all'), query = ref(''), modal = ref(''), toast = ref(''), activeGame = ref<Game | null>(null)
const walletMode=ref<'deposit'|'withdraw'>('deposit')
const authOpen=ref(!!new URLSearchParams(location.search).get('invite')),submitting=ref(false),receiptVersion=ref(0)
const favoriteIds = ref<string[]>([])
try { const saved = JSON.parse(localStorage.getItem('starlight-favorites') || '[]'); if (Array.isArray(saved)) favoriteIds.value = saved.filter((id: unknown) => typeof id === 'string') } catch {}
const filteredGames = computed(() => games.filter(g => (category.value !== 'hot' || g.hot) && (category.value !== 'favorites' || favoriteIds.value.includes(g.id)) && g.name.toLowerCase().includes(query.value.trim().toLowerCase())))
const nav = [{ id: 'home', label: '游戏大厅', icon: House }, { id: 'promotions', label: '福利活动', icon: Gift }, { id: 'records', label: '订单记录', icon: FileText }, { id: 'profile', label: '个人中心', icon: UserRound }]
const categories = [{ id: 'all', label: '全部游戏', icon: LayoutGrid }, { id: 'hot', label: '热门推荐', icon: Flame }, { id: 'favorites', label: '我的收藏', icon: Heart }]
let toastTimer: ReturnType<typeof setTimeout> | undefined
onUnmounted(() => { clearTimeout(toastTimer) })
function notify(message: string) { toast.value = message; clearTimeout(toastTimer); toastTimer = setTimeout(() => toast.value = '', 3200) }
function navigate(id: string) { const path = id === 'admin' ? ADMIN_PATH : '/'; if(location.pathname !== path) history.replaceState(null, '', path); page.value = id; activeGame.value = null; window.scrollTo({ top: 0, behavior: 'smooth' }) }
function openGame(game: Game) { activeGame.value = game; window.scrollTo({ top: 0, behavior: 'smooth' }) }
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
  <div class="app-shell">
    <aside class="sidebar">
      <a href="#" class="brand" @click.prevent="navigate('home')"><span class="brand-symbol"><Sparkles :size="25"/></span><span>星彩娱乐<small>STARLIGHT CLUB</small></span></a>
      <div class="sidebar-caption">发现你的精彩</div>
      <nav class="side-nav"><button v-for="item in nav" :key="item.id" :class="{ active: page === item.id }" @click="navigate(item.id)"><component :is="item.icon" :size="20"/>{{ item.label }}<ChevronRight v-if="page === item.id" :size="15" class="nav-arrow"/></button></nav>
      <button class="side-service" :class="{active:page==='commissions'}" @click="navigate('commissions')"><Gift :size="20"/>邀请与佣金</button><button v-if="user?.role==='admin'" class="side-service" @click="navigate('admin')"><ShieldCheck :size="20"/>管理员后台</button><div class="sidebar-line"></div><div class="sidebar-caption">为你服务</div>
      <button class="side-service" @click="openModal('support')"><Headphones :size="20"/>帮助与客服<span class="online-dot"></span></button>
      <button class="side-service" @click="openModal('mobile')"><Smartphone :size="20"/>手机畅玩</button>
      <div class="sidebar-bottom"><span class="mini-stars">✦</span><strong>让每一刻，都有期待</strong><p>轻松探索，发现更多精彩</p><span class="demo-label">账户服务已接入</span></div>
      <div class="sidebar-footer"><ShieldCheck :size="14"/>账户服务 · 安全登录</div>
    </aside>

    <div class="main-shell">
      <header class="desktop-topbar"><div class="breadcrumb">探索 <ChevronRight :size="14"/><strong>{{ activeGame ? activeGame.name : nav.find(n => n.id === page)?.label || ({wallet:'我的钱包',commissions:'邀请与佣金',admin:'管理员后台'} as Record<string,string>)[page] }}</strong></div><div class="topbar-right"><span class="status"><i></i>欢迎来到星彩</span><button class="icon-button" aria-label="消息通知" @click="openModal('notifications')"><Bell :size="20"/><b class="notification-dot"></b></button><button class="avatar" @click="navigate('profile')">S</button><span v-if="user" class="user-name">{{user.username}}<small>{{user.role==='admin'?'ADMIN':'STAR MEMBER'}}</small></span><button v-else class="primary-button header-login" @click="authOpen=true">登录 / 注册</button></div></header>
      <main>
        <template v-if="page === 'home' && !activeGame">
          <section class="welcome-panel">
            <div class="welcome-heading"><div><div class="mobile-brand"><Sparkles :size="23"/>星彩娱乐</div><p class="eyebrow">WELCOME TO STARLIGHT</p><h1>美好的一天，从精彩开始<span>✦</span></h1><p class="welcome-subtitle">你喜欢的游戏，都在这里。</p></div><div class="balance-box"><span>余额 <button aria-label="刷新余额" @click="refreshBalance"><RefreshCw :size="13"/></button></span><strong>{{ balance.toLocaleString('en-US', { minimumFractionDigits: 2 }) }}</strong><button class="mobile-notification" aria-label="消息通知" @click="openModal('notifications')"><Bell :size="21"/></button></div></div>
            <div class="welcome-controls"><label class="search-box"><Search :size="19"/><input v-model="query" placeholder="搜索你喜欢的游戏" aria-label="搜索游戏"/><button v-if="query" aria-label="清空搜索" @click="query = ''"><X :size="16"/></button><span class="search-shortcut">探索更多</span></label><div class="quick-actions"><button @click="walletMode='deposit';navigate('wallet')"><span><Plus :size="21"/></span>充值</button><button @click="walletMode='withdraw';navigate('wallet')"><span><ArrowUpRight :size="21"/></span>提现</button><button @click="openModal('support')"><span><Headphones :size="21"/></span>在线客服</button><button @click="navigate('records')"><span><FileText :size="21"/></span>订单记录</button></div></div>
          </section>
          <div class="announcement"><span><Volume2 :size="17"/><b>星彩公告</b></span><p>{{site?.announcement||'欢迎来到星彩娱乐。'}}</p><button aria-label="查看公告" @click="openModal('notifications')"><ChevronRight :size="17"/></button></div>
          <section class="promo-banner"><div class="promo-copy"><span class="promo-label">STARLIGHT EXCLUSIVE <i>✧</i></span><h2>每一份热爱，都值得期待</h2><p>新朋友，新体验。一起探索更多精彩。</p><button @click="navigate('promotions')">探索专属福利 <ArrowUpRight :size="16"/></button></div><div class="promo-art" aria-hidden="true"><div class="banner-orbit orbit-a"></div><div class="banner-orbit orbit-b"></div><span class="floating-spark spark-a">✦</span><span class="floating-spark spark-b">✧</span><div class="gift-cube"><div class="gift-bow">∞</div><div class="gift-lid"></div><div class="gift-body"><span>✦</span></div></div><span class="coin coin-a">✦</span><span class="coin coin-b">✦</span><span class="small-sphere"></span></div><div class="banner-dots"><i class="selected"></i><i></i><i></i></div></section>
          <section class="games-section"><div class="section-heading"><div><span class="section-eyebrow">PICK YOUR FAVORITE</span><h2>发现好游戏 <span>总有一款，属于你</span></h2></div><span class="game-count">{{ filteredGames.length }} 款精选游戏</span></div><div class="game-layout"><div class="category-tabs"><button v-for="cat in categories" :key="cat.id" :class="{ selected: category === cat.id }" @click="category = cat.id"><component :is="cat.icon" :size="18"/><span>{{ cat.label }}</span><span v-if="cat.id === 'all'" class="category-count">06</span></button></div><div class="game-grid"><article v-for="game in filteredGames" :key="game.id" class="game-card" :style="{ '--card-bg': game.color, '--accent': game.accent }"><button class="game-card-main" @click="openGame(game)"><span class="game-tag" v-if="game.tag">{{ game.tag }}</span><div class="game-copy"><small>{{ game.english }}</small><h3>{{ game.name }}</h3><p>{{ game.subtitle }}</p><span class="game-enter">进入游戏 <ArrowUpRight :size="14"/></span></div><GameArtwork :game="game"/></button><button class="favorite-button" :class="{ saved: favoriteIds.includes(game.id) }" :aria-label="`${favoriteIds.includes(game.id) ? '取消收藏' : '收藏'}${game.name}`" @click="favorite(game.id)"><Heart :size="16" :fill="favoriteIds.includes(game.id) ? 'currentColor' : 'none'"/></button></article><div v-if="!filteredGames.length" class="empty-state"><Search :size="34"/><h3>{{ category === 'favorites' ? '收藏你喜欢的游戏' : '没有找到相关游戏' }}</h3><p>{{ category === 'favorites' ? '点击游戏卡片上的爱心，即可添加到这里' : '换个关键词再试试吧' }}</p><button class="secondary-button" @click="query = ''; category = 'all'">查看全部游戏</button></div></div></div></section>
          <section class="assurance-strip"><div><ShieldCheck/><span><strong>安心体验</strong><small>用心守护每一次探索</small></span></div><div><Clock3/><span><strong>随时随地</strong><small>电脑手机，无缝切换</small></span></div><div><Headphones/><span><strong>贴心陪伴</strong><small>你的疑问，始终有人在意</small></span></div></section>
        </template>

        <GameRoom v-else-if="activeGame" :key="activeGame.id" :game="activeGame" :balance="balance" :play-settings="site?.plays ?? []" :submitting="submitting" :receipt-version="receiptVersion" :logged-in="!!user" @back="activeGame=null" @records="navigate('records')" @notify="notify" @submit="savePick" @login="authOpen=true"/>

        <AccountPanel v-else-if="['profile','wallet','records','commissions'].includes(page)" :page="page" :wallet-mode="walletMode" @login="authOpen=true" @navigate="navigate" @notify="notify" @logout="signOut"/>
        <AdminPanel v-else-if="page==='admin'" @login="authOpen=true" @notify="notify"/>
        <PromotionsPage v-else-if="page==='promotions'" @navigate="action=>{if(action==='favorites'){navigate('home');category='favorites'}else navigate(action)}"/>

        <footer class="main-footer"><span>✦ STARLIGHT CLUB</span><p>让每一刻，都有期待</p><small>账户中心 · 定时开奖</small></footer>
      </main>
    </div>
    <nav class="mobile-nav"><button v-for="item in [nav[0]!, nav[1]!, {id: 'support', label: '客服', icon: Headphones}, nav[3]!]" :key="item.id" :class="{ active: page === item.id }" @click="item.id === 'support' ? openModal('support') : navigate(item.id)"><component :is="item.icon" :size="23"/><span>{{ item.id === 'home' ? '首页' : item.id === 'profile' ? '我的' : item.label }}</span><i v-if="page === item.id"></i></button></nav>
    <AuthDialog v-if="authOpen" @close="authOpen=false" @success="signedIn"/>
    <Transition name="toast"><div v-if="toast" class="toast-message" role="status"><Check :size="18"/>{{ toast }}</div></Transition>
    <div v-if="modal" class="modal-overlay" @click.self="closeModal" @keydown.esc="closeModal" @keydown="trapFocus"><section ref="dialogRef" class="modal" role="dialog" aria-modal="true" aria-labelledby="dialog-title" tabindex="-1"><button class="modal-close icon-button" aria-label="关闭弹窗" @click="closeModal"><X :size="22"/></button><div class="modal-emblem"><component :is="modal === 'support' ? Headphones : modal === 'notifications' ? Bell : modal === 'mobile' ? Smartphone : Wallet" :size="29"/></div><h2 id="dialog-title">{{ ({support:'你好，需要什么帮助？', notifications:'星彩公告', wallet:'体验账户', withdraw:'提现功能说明', mobile:'随时随地，畅享星彩', about:'关于星彩娱乐'} as Record<string,string>)[modal] }}</h2><template v-if="modal === 'support'"><p>这里是你的自助帮助中心。</p><details open><summary>如何开始体验？</summary><p>在大厅选择游戏，每行选择一个号码，然后点击“确认选号”。</p></details><details><summary>体验数据会保存吗？</summary><p>订单与余额保存在服务器，登录后可跨设备查看。收藏保存在当前浏览器。</p></details><p class="modal-note">当前演示未连接在线客服服务。</p></template><template v-else-if="modal === 'notifications'"><div class="notice-content"><span>产品公告 · 欢迎体验</span><h3>全新星彩游戏大厅，与你见面</h3><p>{{site?.announcement}}</p></div></template><template v-else-if="modal === 'wallet' || modal === 'withdraw'"><p>账户余额与流水由服务器保存。</p><div class="modal-balance"><small>可用余额</small><strong>{{ balance.toLocaleString() }}</strong></div><p>充值与提现尚未接入支付服务。请在钱包页面提交申请。</p></template><template v-else-if="modal === 'mobile'"><p>使用手机浏览器打开此站点，即可自动切换为手机布局。</p><p class="modal-note">本地预览时，手机和电脑需连接同一 Wi-Fi，并使用开发服务器显示的 Network 地址。</p></template><template v-else><p>星彩娱乐支持电脑和手机访问。</p><p>号码由独立定时服务生成，账户与订单由 Go 后端保存。</p></template><button class="primary-button modal-done" @click="closeModal">知道了</button></section></div>
  </div>
</template>
