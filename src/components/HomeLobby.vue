<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { Bell, ChevronRight, CircleDollarSign, Clock3, Crown, FilePenLine, Flame, Headphones, Heart, Pause, Play, RefreshCw, Search, Volume2, Wallet, X } from 'lucide-vue-next'
import { games, isGameAvailable, type Game } from '../data'
import { getJSON, type Session } from '../api/games'
import { sessionPeriod } from '../lib/sessionSchedule'
import GameArtwork from './GameArtwork.vue'

const props = defineProps<{ balance: number; username?: string; announcement?: string; favoriteIds: string[]; category: string }>()
const emit = defineEmits<{
 game: [game: Game]; session: [id: string]; navigate: [page: string]; wallet: [mode: 'deposit' | 'withdraw'];
 category: [id: string]; help: []; notice: []; login: []; refresh: []; favorite: [id: string]; notify: [message: string]
}>()
const query = ref(''), expandedGame = ref('')
const category = computed({get:()=>props.category,set:(value:string)=>emit('category',value)})
const sessions = ref<Session[]>([]), loading = ref(true), error = ref('')
const sessionFavorites = ref<Record<string,string[]>>({lhc:[],k3:[]})
const favoriteKeys: Record<string,string> = {lhc:'starlight-marksix-favorites',k3:'starlight-k3-favorites'}
for (const [gameID,key] of Object.entries(favoriteKeys)) {
 try {
  const saved: unknown = JSON.parse(localStorage.getItem(key) || '[]')
  if (Array.isArray(saved)) sessionFavorites.value[gameID] = saved.filter((id): id is string => typeof id === 'string')
 } catch {}
}
const controller = new AbortController()
async function loadSessions() {
 loading.value = true; error.value = ''
 try {
  const data = await getJSON<{ sessions: Session[] }>('/api/v1/games', AbortSignal.any([controller.signal, AbortSignal.timeout(8000)]))
  sessions.value = data.sessions.filter(session => session.game_id === 'lhc' || session.game_id === 'k3')
  if (!sessions.value.length) error.value = '暂无可用场次'
 } catch (e) { if (!controller.signal.aborted) error.value = (e as Error).message }
 finally { loading.value = false }
}
const promotions = [
 { src: '/agency-partnership-banner.webp', text: '全民代理模式，月入百万不是梦，全网首推', action: 'commissions', background: '#f7f0e3' },
 { src: '/usdt-first-deposit-banner.webp', text: '首冲usdt充值 每日首存赠送5% 安全 稳定 0风险', action: 'deposit', background: '#291382' },
]
const promotionIndex = ref(0), promotionPaused = ref(false), promotionHovered = ref(false), promotionFocused = ref(false)
let promotionTimer: ReturnType<typeof setInterval> | undefined
function startPromotionTimer() {
 clearInterval(promotionTimer)
 promotionTimer = setInterval(() => {
  if (!document.hidden && !promotionPaused.value && !promotionHovered.value && !promotionFocused.value) promotionIndex.value = (promotionIndex.value + 1) % promotions.length
 }, 5000)
}
function selectPromotion(index: number) { promotionIndex.value = index; startPromotionTimer() }
function openPromotion(action: string) { if (action === 'deposit') emit('wallet', 'deposit'); else emit('navigate', action) }
function promotionFocusOut(event: FocusEvent) { promotionFocused.value = event.currentTarget instanceof HTMLElement && event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget) }
onMounted(() => { void loadSessions(); startPromotionTimer() })
onUnmounted(() => { controller.abort(); clearInterval(promotionTimer) })
const search = computed(() => query.value.trim().toLowerCase().replaceAll('快3','快三'))
const regions = computed(() => sessions.value.filter(session =>
 (!search.value || session.game_name.toLowerCase().includes(search.value)) &&
 (category.value !== 'favorites' || props.favoriteIds.includes(session.game_id) || isSessionFavorite(session))
))
const regionsFor = (gameID:string) => regions.value.filter(session => session.game_id === gameID)
// Match a child session without hiding its parent game tile.
const visibleGames = computed(() => games.filter(game =>
 (category.value !== 'hot' || isGameAvailable(game.id) && game.hot) &&
 (category.value !== 'favorites' || isGameAvailable(game.id) && (props.favoriteIds.includes(game.id) || regionsFor(game.id).length > 0)) &&
 (!search.value || game.name.toLowerCase().includes(search.value) || !isGameAvailable(game.id) && '敬请期待'.includes(search.value) || regionsFor(game.id).length > 0)
))
const primaryGames = computed(() => visibleGames.value.filter(game => game.id === 'lhc' || game.id === 'k3'))
const remainingGames = computed(() => visibleGames.value.filter(game => game.id !== 'lhc' && game.id !== 'k3').sort((a,b)=>['ssc','pk10','ffc','pc28'].indexOf(a.id)-['ssc','pk10','ffc','pc28'].indexOf(b.id)))
const showRegions = (gameID:string) => expandedGame.value === gameID || !!search.value || category.value === 'favorites'
const sessionGroups = computed(() => primaryGames.value.filter(game => showRegions(game.id)).map(game => ({game,sessions:regionsFor(game.id)})))
const regionID = (gameID:string) => gameID === 'lhc' ? 'lobby-marksix-regions' : 'lobby-k3-regions'
const filters = [{ id: 'all', label: '彩票', icon: CircleDollarSign }, { id: 'hot', label: '热门', icon: Flame }, { id: 'favorites', label: '收藏', icon: Heart }]
const regionColors = ['#a932bd', '#8859d7', '#df4aa3', '#15964f', '#e56591', '#209cb8', '#427cce', '#c77727', '#488f71']
function regionColor(session:Session) {
 const index = sessions.value.filter(item => item.game_id === session.game_id).findIndex(item => item.id === session.id)
 return regionColors[index % regionColors.length]
}
function selectGame(game: Game) {
 if (!isGameAvailable(game.id)) return
 if (game.id === 'lhc' || game.id === 'k3') expandedGame.value = expandedGame.value === game.id ? '' : game.id
 else emit('game', game)
}
function isSessionFavorite(session:Session) {return sessionFavorites.value[session.game_id]?.includes(session.id) ?? false}
function favoriteSession(session:Session) {
 const favorites = sessionFavorites.value[session.game_id] ?? []
 sessionFavorites.value[session.game_id] = favorites.includes(session.id) ? favorites.filter(value => value !== session.id) : [...favorites,session.id]
 try { localStorage.setItem(favoriteKeys[session.game_id]!, JSON.stringify(sessionFavorites.value[session.game_id])) }
 catch { emit('notify', '浏览器未允许保存收藏，本次浏览仍可使用。') }
}
</script>

<template>
 <div class="compact-lobby">
  <section class="lobby-header" aria-label="账户与快捷入口">
   <div class="lobby-topline">
    <a class="lobby-logo" href="/" aria-label="金沙首页" @click.prevent="emit('navigate','home')"><img src="/js-logo-mobile.png" alt="JS 金沙"/></a>
    <div class="lobby-account">
     <template v-if="username"><button class="lobby-balance" aria-label="查看钱包余额" @click="emit('navigate','wallet')"><span>{{username}}</span><b>¥ {{balance.toLocaleString('en-US',{minimumFractionDigits:2,maximumFractionDigits:2})}}</b></button><button class="lobby-icon" aria-label="刷新余额" @click="emit('refresh')"><RefreshCw :size="16"/></button></template>
     <button v-else class="lobby-login" @click="emit('login')">登录 / 注册</button>
     <button class="lobby-icon" aria-label="消息通知" @click="emit('notice')"><Bell :size="20"/></button>
    </div>
   </div>
   <label class="lobby-search"><Search :size="18"/><input v-model="query" aria-label="搜索游戏" placeholder="输入游戏名称"/><button v-if="query" aria-label="清空搜索" @click="query='' "><X :size="16"/></button></label>
   <div class="lobby-shortcuts">
    <button @click="emit('wallet','deposit')"><span><CircleDollarSign/></span>充值</button>
    <button @click="emit('wallet','withdraw')"><span><Wallet/></span>提款</button>
    <button @click="emit('help')"><span><Headphones/></span>帮助中心</button>
    <button @click="emit('navigate','records')"><span><FilePenLine/></span>投注记录</button>
   </div>
  </section>
  <button class="lobby-notice" @click="emit('notice')"><Volume2 :size="17"/><span>{{announcement || '欢迎来到金沙娱乐。请留意各场次开奖时间。'}}</span><ChevronRight :size="16"/></button>
  <section class="lobby-promotion" aria-label="首页活动轮播" aria-roledescription="轮播" @mouseenter="promotionHovered=true" @mouseleave="promotionHovered=false" @focusin="promotionFocused=true" @focusout="promotionFocusOut">
   <div class="lobby-promotion-stage">
    <button v-for="(promotion,index) in promotions" :key="promotion.src" class="lobby-promotion-slide" :class="{active:promotionIndex===index}" :style="{background:promotion.background}" :aria-label="promotion.text" :aria-hidden="promotionIndex!==index" :tabindex="promotionIndex===index?0:-1" @click="openPromotion(promotion.action)">
     <img :src="promotion.src" :alt="promotion.text" width="2160" height="720" :fetchpriority="index===0?'high':'auto'" decoding="async"/>
    </button>
   </div>
   <div class="lobby-promotion-controls">
    <button v-for="(promotion,index) in promotions" :key="promotion.src" class="lobby-promotion-dot" :class="{active:promotionIndex===index}" :aria-label="`显示第${index+1}张：${promotion.text}`" :aria-pressed="promotionIndex===index" @click="selectPromotion(index)"><span/></button>
    <button class="lobby-promotion-pause" :aria-label="promotionPaused?'播放轮播':'暂停轮播'" @click="promotionPaused=!promotionPaused"><Play v-if="promotionPaused" :size="11"/><Pause v-else :size="11"/></button>
   </div>
  </section>
  <section class="lobby-catalog" aria-label="游戏大厅">
   <nav class="lobby-rail" aria-label="游戏分类"><button v-for="filter in filters" :key="filter.id" :class="{active:category===filter.id}" :aria-pressed="category===filter.id" @click="category=filter.id"><span><component :is="filter.icon" :size="23"/></span>{{filter.label}}</button></nav>
   <div class="lobby-games">
    <div class="lobby-feature-grid">
     <article v-for="game in primaryGames" :key="game.id" class="lobby-game-tile" :class="[game.id,{expanded:showRegions(game.id)}]" :style="{'--accent':game.accent}">
      <button class="lobby-tile-link" :aria-label="`${game.name}场次`" :aria-expanded="showRegions(game.id)" :aria-controls="regionID(game.id)" @click="selectGame(game)"><h2>{{game.name}}</h2><template v-if="game.id==='lhc'"><div class="lobby-jackpot" aria-hidden="true"><Crown/><span>六合彩</span><b>MARK SIX</b><i>✦ ✦ ✦</i></div></template><GameArtwork v-else :game="game"/></button>
      <button class="lobby-tile-favorite" :aria-label="`${favoriteIds.includes(game.id)?'取消收藏':'收藏'}${game.name}`" :aria-pressed="favoriteIds.includes(game.id)" @click="emit('favorite',game.id)"><Heart :size="13" :fill="favoriteIds.includes(game.id)?'currentColor':'none'"/></button>
     </article>
    </div>
    <section v-for="group in sessionGroups" :key="group.game.id" class="lobby-session-group" :class="group.game.id" :aria-label="`${group.game.name}场次列表`">
     <h3 class="lobby-session-heading">{{group.game.name}}<span>{{group.sessions.length}}个场次</span></h3>
     <div :id="regionID(group.game.id)" class="lobby-regions" :aria-label="`${group.game.name}场次`">
      <p v-if="loading" class="lobby-region-status" role="status">正在加载场次…</p>
      <div v-else-if="error" class="lobby-region-status" role="alert"><p>{{error}}</p><button @click="loadSessions"><RefreshCw :size="13"/>重新加载</button></div>
      <p v-else-if="!group.sessions.length" class="lobby-region-status">暂无可用场次</p>
      <template v-else><article v-for="session in group.sessions" :key="session.id" class="lobby-region" :style="{'--medal-color':regionColor(session)}">
       <button class="lobby-region-link" :aria-label="`进入${session.game_name}，${sessionPeriod(session)}`" @click="emit('session',session.id)"><span class="lobby-medal" aria-hidden="true"><i>{{group.game.id==='lhc'?'✦ MARK SIX ✦':'✦ LUCKY DICE ✦'}}</i><b>{{session.game_name.replace(group.game.name,'')}}</b><small>{{group.game.name}}</small><span>{{group.game.id==='lhc'?'★ ★ ★':'⚄ ⚂ ⚁'}}</span></span><strong>{{session.game_name}}</strong><small class="lobby-region-period">{{sessionPeriod(session)}}</small></button>
       <button class="lobby-region-favorite" :aria-label="`${isSessionFavorite(session)?'取消收藏':'收藏'}${session.game_name}`" :aria-pressed="isSessionFavorite(session)" @click="favoriteSession(session)"><Heart :size="12" :fill="isSessionFavorite(session)?'currentColor':'none'"/></button>
      </article></template>
     </div>
    </section>
    <div class="lobby-other-grid">
     <article v-for="game in remainingGames" :key="game.id" class="lobby-game-tile" :class="[game.id,{'coming-soon':!isGameAvailable(game.id)}]" :style="{'--accent':game.accent}" :aria-label="!isGameAvailable(game.id)?`${game.name}，敬请期待`:undefined" :aria-disabled="!isGameAvailable(game.id)||undefined">
      <template v-if="isGameAvailable(game.id)"><button class="lobby-tile-link" :aria-label="`进入${game.name}`" @click="selectGame(game)"><h2>{{game.name}}</h2><span class="lobby-tile-caption">{{game.subtitle}}</span><GameArtwork :game="game"/></button><button class="lobby-tile-favorite" :aria-label="`${favoriteIds.includes(game.id)?'取消收藏':'收藏'}${game.name}`" :aria-pressed="favoriteIds.includes(game.id)" @click="emit('favorite',game.id)"><Heart :size="13" :fill="favoriteIds.includes(game.id)?'currentColor':'none'"/></button></template>
      <div v-else class="lobby-coming-soon"><Clock3 :size="30" aria-hidden="true"/><h2>敬请期待</h2></div>
     </article>
    </div>
    <div v-if="!visibleGames.length" class="lobby-empty"><Search :size="27"/><strong>{{category==='favorites'?'暂无收藏':'未找到相关游戏'}}</strong><p>{{category==='favorites'?'点击卡片上的爱心收藏游戏':'试试搜索游戏或地区名称'}}</p><button @click="category='all';query=''">查看全部游戏</button></div>
   </div>
  </section>
 </div>
</template>

<style scoped>
.lobby-other-grid .lobby-game-tile.coming-soon{background:linear-gradient(135deg,#f4f1fa,#e7e3f0);border-color:#eee9f5}.lobby-coming-soon{display:flex;height:100%;flex-direction:column;align-items:center;justify-content:center;gap:10px;color:#8b819e}.lobby-coming-soon svg{color:#a99dbb;stroke-width:1.5}.lobby-coming-soon h2{margin:0;font-size:16px;font-weight:500;letter-spacing:2px}
.lobby-session-heading{display:flex;align-items:center;gap:7px;margin:8px 2px 3px;font-size:12px;color:#756279;font-weight:600}.lobby-session-heading span{font-size:10px;font-weight:400;color:#9a889c}.lobby-session-group.k3 .lobby-regions{grid-template-columns:repeat(3,minmax(0,1fr));background:#edf5ef}.lobby-session-group.k3 .lobby-region{background:radial-gradient(ellipse at 50% 15%,#d9f7be,transparent 78%),linear-gradient(135deg,#b5e9d3,#9bddcf);border-color:#d9f6e4}.lobby-session-group.k3 .lobby-region-link>strong{color:#246c61;text-shadow:0 1px 1px #fff9}.lobby-session-group.k3 .lobby-region-period{color:#407764}.lobby-session-group.k3 .lobby-region-favorite{color:#518e7b}.lobby-session-group.k3 .lobby-region-favorite[aria-pressed=true]{color:#c94888}.lobby-game-tile.k3.expanded{box-shadow:inset 0 0 0 2px #279b85}

.compact-lobby{background:#fff;color:#4c4861;overflow:hidden;border-radius:15px;box-shadow:0 4px 25px #53508a0a}
.lobby-header{padding:15px 25px 9px;color:#fff;background:radial-gradient(ellipse at 100% 0,#a051f2,transparent 70%),linear-gradient(135deg,#7951f5,#505aee 70%,#3f85e9)}
.lobby-topline{display:flex;align-items:center;justify-content:space-between;gap:12px;min-height:40px}.lobby-logo{display:none;align-items:center;gap:6px;flex-shrink:0}.lobby-logo img{display:block;width:118px;height:auto;border-radius:4px}.lobby-logo strong{font-size:21px;font-weight:800;letter-spacing:.3px;text-shadow:0 2px 3px #342a5650}.lobby-logo svg{filter:drop-shadow(0 2px 2px #412b5b60)}.lobby-account{margin-left:auto;display:flex;align-items:center;gap:5px;min-width:0}.lobby-balance{padding:0;text-align:right;min-width:0}.lobby-balance span{display:block;overflow:hidden;max-width:150px;text-overflow:ellipsis;white-space:nowrap;font-size:11px;opacity:.8}.lobby-balance b{display:block;font-size:13px;font-weight:500;font-variant-numeric:tabular-nums}.lobby-icon{display:grid;place-items:center;width:30px;height:36px;padding:0}.lobby-login{font-size:12px;border:1px solid #ffffff60;padding:5px 10px;border-radius:20px}.lobby-search{height:36px;display:flex;align-items:center;gap:6px;margin:12px auto 10px;background:linear-gradient(90deg,#b7c6ff87,#d4b6fa99);border:1px solid #fff2;border-radius:30px;padding:0 17px;max-width:750px;color:#fff}.lobby-search input{width:100%;min-width:0;outline:none;background:none;border:0;color:#fff;font-size:13px}.lobby-search input::placeholder{color:#f1eaff}.lobby-search:focus-within{box-shadow:0 0 0 2px #fff8}.lobby-search button{padding:0;display:grid;place-items:center;width:26px;height:30px}.lobby-shortcuts{display:grid;grid-template-columns:repeat(4,1fr);max-width:750px;margin:auto}.lobby-shortcuts button{display:flex;flex-direction:column;align-items:center;gap:3px;font-size:12px;padding:8px 2px 2px;color:#fff;text-shadow:0 1px 3px #33407466}.lobby-shortcuts button>span{display:grid;place-items:center;width:36px;height:32px;color:#e2ffff;filter:drop-shadow(0 0 5px #b1ffffa0)}.lobby-shortcuts svg{width:26px;height:26px;stroke-width:1.5}
.lobby-notice{display:flex;align-items:center;gap:8px;width:100%;height:28px;padding:0 12px;background:#f5fcfd;color:#8a68cc;text-align:left}.lobby-notice span{white-space:nowrap;overflow:hidden;text-overflow:ellipsis;font-size:12px;color:#5d5869;flex:1}
.lobby-promotion{position:relative;width:100%;line-height:0;overflow:hidden;background:#291382;border-bottom:4px solid #fff}.lobby-promotion-stage{position:relative;width:100%;aspect-ratio:3/1;max-height:260px}.lobby-promotion-slide{position:absolute;inset:0;width:100%;height:100%;padding:0;opacity:0;visibility:hidden;pointer-events:none;transition:opacity .45s ease,visibility .45s}.lobby-promotion-slide.active{opacity:1;visibility:visible;pointer-events:auto}.lobby-promotion img{display:block;width:100%;height:100%;object-fit:contain}.lobby-promotion-slide:focus-visible{outline:3px solid #fff;outline-offset:-5px}.lobby-promotion-controls{position:absolute;bottom:5px;left:50%;transform:translateX(-50%);display:flex;align-items:center;padding:0 4px;border-radius:15px;background:#211b3c70;color:#fff}.lobby-promotion-controls button{display:grid;place-items:center;width:24px;height:22px;padding:0}.lobby-promotion-dot span{width:6px;height:6px;border-radius:8px;background:#ffffff80;transition:width .2s,background .2s}.lobby-promotion-dot.active span{width:15px;background:#fff}.lobby-promotion-controls button:focus-visible{outline:2px solid #fff;border-radius:10px;outline-offset:-2px}
.lobby-catalog{display:flex;gap:12px;padding:6px 15px 22px 0}.lobby-rail{flex:0 0 70px;display:flex;align-items:center;flex-direction:column;gap:8px;padding-top:3px}.lobby-rail button{width:56px;min-height:66px;border-radius:0 15px 15px 0;display:flex;align-items:center;justify-content:center;flex-direction:column;gap:4px;color:#989eab;font-size:11px}.lobby-rail button>span{width:31px;height:31px;display:grid;place-items:center}.lobby-rail button.active{background:linear-gradient(145deg,#42d4d4,#7088f6 70%,#8c58e6);color:#fff;box-shadow:0 2px 6px #747ff040}.lobby-rail button.active>span{border:1px solid #d9ffff;border-radius:50%;box-shadow:0 0 0 3px #fff2}.lobby-games{flex:1;min-width:0}.lobby-feature-grid,.lobby-other-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:5px}.lobby-game-tile{position:relative;min-width:0;height:153px;border:1px solid #ffffffa0;border-radius:5px;overflow:hidden;background:linear-gradient(135deg,#ffdc89,#ffc47c);isolation:isolate}.lobby-game-tile:after{content:'';position:absolute;inset:40% -15% -45% 15%;border:1px solid #fff7;border-radius:50%;transform:rotate(-15deg);pointer-events:none;z-index:-1}.lobby-game-tile.k3{background:linear-gradient(120deg,#d0f59c,#3bd5c2 65%,#2bbad5)}.lobby-game-tile.ssc{background:linear-gradient(120deg,#ffd799,#f7a55d)}.lobby-game-tile.ffc{background:linear-gradient(120deg,#d8b5fa,#a180ea)}.lobby-game-tile.pk10{background:linear-gradient(120deg,#acf2d9,#6ebcec)}.lobby-game-tile.pc28{background:linear-gradient(120deg,#ffccdd,#f29bbc)}.lobby-tile-link{width:100%;height:100%;padding:0;text-align:left;position:relative;display:block;overflow:hidden}.lobby-tile-link h2{position:absolute;right:12px;top:9px;z-index:2;font-size:22px;font-weight:700;color:#514e57;letter-spacing:-.5px}.lobby-tile-caption{position:absolute;left:13px;bottom:12px;font-size:11px;color:#63566d;z-index:2}.lobby-tile-favorite{position:absolute;left:7px;top:7px;z-index:3;width:25px;height:25px;border-radius:50%;display:grid;place-items:center;background:#fff5;color:#695179}.lobby-tile-favorite[aria-pressed=true]{color:#c74483;background:#fff9}.lobby-game-tile :deep(.game-art){left:15px;right:auto;bottom:-6px;transform:scale(1.02);transform-origin:left bottom}.lobby-game-tile :deep(.ticket-art){background:linear-gradient(140deg,#fffdf6,#f6ddb2)}.lobby-other-grid{margin-top:5px}.lobby-other-grid .lobby-tile-link h2{top:13px;right:12px}.lobby-other-grid .lobby-game-tile :deep(.game-art){left:auto;right:7px;transform:scale(.8);transform-origin:right bottom;bottom:15px}.lobby-other-grid .lobby-tile-caption{max-width:45%;line-height:1.6}.lobby-jackpot{position:absolute;left:17px;bottom:7px;display:flex;align-items:center;flex-direction:column;width:145px;color:#936021;transform:rotate(-7deg)}.lobby-jackpot svg{width:62px;height:43px;fill:#fff5b0;stroke:#bd944c;filter:drop-shadow(1px 2px #fff)}.lobby-jackpot>span{font-size:25px;font-weight:900;color:#a94c30;text-shadow:1px 1px #fff8b5,-1px -1px #fff8b5,2px 3px #694a36;letter-spacing:2px;line-height:1.15}.lobby-jackpot b{font-size:18px;color:#fff9c2;text-shadow:1px 2px #754f3c,-1px -1px #754f3c;letter-spacing:1px;line-height:1.2}.lobby-jackpot i{font-size:15px;font-style:normal;letter-spacing:9px;color:#e4a538;text-shadow:1px 1px #fff}.lobby-game-tile.expanded{box-shadow:inset 0 0 0 2px #f8ad53}.lobby-regions{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:3px;margin-top:5px;padding:0;background:#f6f1ec}.lobby-region{position:relative;min-width:0;background:radial-gradient(ellipse at 50% 15%,#ffcf87,transparent 78%),linear-gradient(135deg,#ffb67b,#ffd095);border:1px solid #ffe5ba;border-radius:4px;overflow:hidden}.lobby-region-link{width:100%;height:149px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:7px;padding:10px 3px 7px}.lobby-region-period{font-size:11px;line-height:1.2;color:#795231;white-space:nowrap}.lobby-region-link>strong{font-size:14px;color:#fffef4;font-weight:600;text-shadow:0 1px 2px #b3793f;white-space:nowrap}.lobby-medal{width:79px;height:79px;border-radius:50%;border:2px solid #f4dba0;outline:1px solid #ffffff80;box-shadow:inset 0 0 0 3px #ffffff45,0 3px 4px #9f652e33;background:radial-gradient(ellipse at 35% 0,#ffffffa0,transparent 50%),linear-gradient(150deg,var(--medal-color),color-mix(in srgb,var(--medal-color),#3b2358 35%));display:flex;align-items:center;justify-content:center;flex-direction:column;color:#fff;transform:rotate(-8deg);text-shadow:1px 1px #49346366}.lobby-medal>i{font-size:6px;font-style:normal;letter-spacing:1px;opacity:.85;line-height:1.5}.lobby-medal>b{font-size:18px;font-weight:800;line-height:1.25}.lobby-medal>small{font-size:12px;letter-spacing:2px;line-height:1.4}.lobby-medal>span{font-size:7px;letter-spacing:2px;color:#fff2bc}.lobby-region-favorite{position:absolute;right:2px;top:2px;display:grid;place-items:center;width:28px;height:28px;color:#fff9e8;padding:0;z-index:1}.lobby-region-favorite[aria-pressed=true]{color:#c94888}.lobby-region-status{grid-column:1/-1;padding:28px 10px;font-size:12px;text-align:center;color:#8c6e60;background:#fff5e5;line-height:1.8}.lobby-region-status button{display:inline-flex;gap:5px;align-items:center;background:#fff;border:1px solid #efcda0;border-radius:5px;padding:5px 10px;margin-top:8px}.lobby-empty{display:flex;flex-direction:column;align-items:center;text-align:center;gap:12px;padding:45px 8px;color:#9687b2;font-size:12px}.lobby-empty strong{font-size:15px}.lobby-empty button{background:#f1eaff;padding:10px 18px;border-radius:6px;color:#7953ba}
@media(min-width:1200px){.lobby-feature-grid .lobby-game-tile{height:165px}.lobby-regions{grid-template-columns:repeat(4,minmax(0,1fr))}.lobby-region-link{height:149px}.lobby-region-link>strong{font-size:12px}.lobby-other-grid{grid-template-columns:repeat(4,minmax(0,1fr))}.lobby-other-grid .lobby-game-tile{height:145px}.lobby-other-grid .lobby-tile-link h2{font-size:18px}.lobby-other-grid .lobby-tile-caption{display:none}.lobby-other-grid .lobby-game-tile :deep(.game-art){transform:scale(.73);bottom:0}}
@media(max-width:760px){
 .compact-lobby{border-radius:0;box-shadow:none;min-height:calc(100dvh - 62px)}.lobby-header{padding:8px 10px 5px}.lobby-topline{min-height:39px;gap:4px}.lobby-logo{display:flex;gap:4px}.lobby-logo img{width:118px}.lobby-logo strong{font-size:17px}.lobby-logo svg{width:20px}.lobby-account{gap:0}.lobby-balance span{max-width:95px;font-size:9px}.lobby-balance b{font-size:11px}.lobby-icon{width:28px}.lobby-login{font-size:11px;padding:4px 8px}.lobby-search{height:33px;margin:10px 0 7px;padding:0 14px;justify-content:center}.lobby-search input{flex:0 1 120px;font-size:12px}.lobby-search:focus-within input{flex:1}.lobby-shortcuts button{font-size:11px;gap:3px;padding:7px 0 0;min-height:59px}.lobby-shortcuts button>span{height:29px}.lobby-shortcuts svg{width:25px;height:25px}.lobby-notice{height:26px;padding:0 4px;gap:5px}.lobby-notice span{font-size:12px}.lobby-promotion{border-bottom-width:3px}.lobby-catalog{gap:4px;padding:3px 7px 15px 0}.lobby-rail{flex-basis:48px;gap:8px}.lobby-rail button{width:44px;min-height:62px;font-size:10px;gap:4px;border-radius:0 12px 12px 0}.lobby-rail button>span{width:26px;height:26px}.lobby-rail svg{width:21px;height:21px}.lobby-feature-grid,.lobby-other-grid{gap:3px}.lobby-game-tile{height:104px;border-radius:4px}.lobby-tile-link h2{font-size:16px;top:6px;right:6px}.lobby-game-tile :deep(.game-art){transform:scale(.66);left:0;bottom:-7px}.lobby-tile-favorite{width:24px;height:24px;left:3px;top:3px;background:#fff4}.lobby-jackpot{width:108px;bottom:3px;left:3px}.lobby-jackpot svg{width:47px;height:35px}.lobby-jackpot>span{font-size:20px;letter-spacing:0}.lobby-jackpot b{font-size:14px;letter-spacing:.3px}.lobby-jackpot i{font-size:10px;letter-spacing:7px}.lobby-regions{grid-template-columns:repeat(3,minmax(0,1fr));gap:2px;margin-top:3px}.lobby-region-link{height:108px;padding:5px 1px;gap:5px}.lobby-medal{width:55px;height:55px;border-width:1px;box-shadow:inset 0 0 0 2px #ffffff45,0 2px 3px #9f652e33}.lobby-medal>i{font-size:4px;letter-spacing:.5px}.lobby-medal>b{font-size:13px}.lobby-medal>small{font-size:9px;letter-spacing:1px}.lobby-medal>span{font-size:5px;letter-spacing:1px}.lobby-region-link>strong{font-size:clamp(9px,2.7vw,12px)}.lobby-region-period{font-size:10px;line-height:1.2}.lobby-region-favorite{width:25px;height:25px;right:0;top:0}.lobby-region-favorite svg{width:11px;height:11px}.lobby-other-grid{margin-top:3px}.lobby-other-grid .lobby-game-tile{height:103px}.lobby-other-grid .lobby-tile-link h2{top:6px;right:6px;font-size:16px}.lobby-other-grid .lobby-tile-caption{display:none}.lobby-other-grid .lobby-game-tile :deep(.game-art){transform:scale(.56);bottom:0;right:0}
}
@media(prefers-reduced-motion:reduce){.compact-lobby *{scroll-behavior:auto;transition:none}}
</style>
