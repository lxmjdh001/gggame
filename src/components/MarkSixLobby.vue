<script setup lang="ts">
import {computed, onMounted, onUnmounted, ref} from 'vue'
import {ArrowUpRight, ChevronLeft, Heart, LayoutGrid, RefreshCw} from 'lucide-vue-next'
import type {Game} from '../data'
import {getJSON, type Session} from '../api/games'
import {closeAhead, sessionPeriod} from '../lib/sessionSchedule'
import GameArtwork from './GameArtwork.vue'
defineProps<{game: Game}>()
const emit = defineEmits<{back: []; select: [id: string]}>()
const sessions = ref<Session[]>([]), loading = ref(true), error = ref('')
const favoritesOnly = ref(false), favoriteIds = ref<string[]>([]), favoriteNotice = ref('')
const favoritesKey = 'starlight-marksix-favorites'
try {
 const saved = JSON.parse(localStorage.getItem(favoritesKey) || '[]')
 if (Array.isArray(saved)) favoriteIds.value = saved.filter((id: unknown): id is string => typeof id === 'string')
} catch {}
const visibleSessions = computed(() => sessions.value.map((session,index) => ({session,index})).filter(({session}) => !favoritesOnly.value || favoriteIds.value.includes(session.id)))
function favorite(id: string) {
 favoriteIds.value = favoriteIds.value.includes(id) ? favoriteIds.value.filter(value => value !== id) : [...favoriteIds.value,id]
 favoriteNotice.value = ''
 try {localStorage.setItem(favoritesKey, JSON.stringify(favoriteIds.value))}
 catch {favoriteNotice.value = '浏览器未允许保存收藏，本次浏览仍可使用。'}
}
const controller = new AbortController()
async function load() {
 loading.value = true; error.value = ''
 try {
  const data = await getJSON<{sessions: Session[]}>('/api/v1/games', AbortSignal.any([controller.signal, AbortSignal.timeout(8000)]))
  sessions.value = data.sessions.filter(s => s.game_id === 'lhc')
  if (!sessions.value.length) throw new Error('暂时没有可用场次')
 } catch (e) {if (!controller.signal.aborted) error.value = (e as Error).message}
 finally {loading.value = false}
}
function enter(id: string) {emit('select', id); window.scrollTo({top: 0, behavior: 'smooth'})}
onMounted(load)
onUnmounted(() => controller.abort())
</script>

<template>
 <section class="marksix-lobby">
  <button class="back-link" @click="emit('back')"><ChevronLeft :size="17"/>返回游戏大厅</button>
  <div class="room-heading"><div><p class="section-eyebrow">STARLIGHT · MARK SIX</p><h1>六合彩<span class="room-type">八大场次</span></h1><p>北京时间10月15日12:00起按下方周期开奖。极速场提前10秒封盘，其余七个场次提前1分钟封盘。</p></div></div>
  <div v-if="error" class="room-error" role="alert">{{error}}<button @click="load"><RefreshCw :size="15"/>重试</button></div>
  <div v-else-if="loading" class="room-loading">正在加载六合彩场次…</div>
  <template v-else>
  <div class="region-filters" aria-label="六合彩场次筛选"><button :class="{active:!favoritesOnly}" :aria-pressed="!favoritesOnly" @click="favoritesOnly=false"><LayoutGrid :size="14"/>全部场次<span>{{sessions.length}}</span></button><button :class="{active:favoritesOnly}" :aria-pressed="favoritesOnly" @click="favoritesOnly=true"><Heart :size="14"/>我的收藏<span>{{sessions.filter(s=>favoriteIds.includes(s.id)).length}}</span></button></div>
  <p v-if="favoriteNotice" class="favorite-notice" role="status">{{favoriteNotice}}</p>
  <div class="marksix-region-grid">
   <article v-for="{session,index:i} in visibleSessions" :key="session.id" class="game-card" :style="{'--card-bg': ['#fff1ce','#e9e2ff','#ffead8','#dbf4ed','#dceeff','#ffe2e9','#e0eaff','#f8ebd7'][i%8], '--accent': ['#b98c31','#8860e9','#ee7d35','#2caf94','#408dde','#e97499','#527fce','#ba8439'][i%8]}">
    <button class="game-card-main" :aria-label="`进入${session.game_name}，${sessionPeriod(session)}`" @click="enter(session.id)"><div class="game-copy"><small>MARK SIX · {{String(i+1).padStart(2,'0')}}</small><h3>{{session.game_name}}</h3><p class="region-period">{{sessionPeriod(session)}}</p><p>提前{{closeAhead(session)}}封盘</p><p class="region-format">49选7 · 六正一特</p><span class="game-enter">进入游戏 <ArrowUpRight :size="15"/></span></div><GameArtwork :game="game"/></button>
    <button class="region-favorite" :class="{saved:favoriteIds.includes(session.id)}" :aria-label="`${favoriteIds.includes(session.id)?'取消收藏':'收藏'}${session.game_name}`" :aria-pressed="favoriteIds.includes(session.id)" @click="favorite(session.id)"><Heart :size="15" :fill="favoriteIds.includes(session.id)?'currentColor':'none'"/></button>
   </article>
  </div>
  <div v-if="!visibleSessions.length" class="region-empty"><Heart :size="26"/><p>还没有收藏场次</p><span>点击卡片右上角的爱心，将喜欢的场次收藏在这里。</span><button class="secondary-button" @click="favoritesOnly=false">查看全部场次</button></div>
  </template>
 </section>
</template>

<style scoped>
.region-filters{display:flex;align-items:center;gap:8px;margin-bottom:16px}
.region-filters button{display:flex;align-items:center;gap:6px;padding:9px 12px;border:1px solid #e9e4f0;border-radius:9px;font-size:11px;color:#93869f;background:#fff}
.region-filters button.active{background:#eee8fc;border-color:#e2d7f7;color:#7957ba}
.region-filters button span{font-size:10px;opacity:.7;margin-left:2px}
.region-favorite{position:absolute;z-index:3;top:10px;right:10px;width:30px;height:30px;display:grid;place-items:center;border-radius:50%;background:#ffffff87;color:color-mix(in srgb,var(--accent),#625068 25%);transition:background .2s,transform .2s}
.region-favorite:hover{background:#ffffffd9;transform:scale(1.06)}
.region-favorite.saved{color:var(--accent);background:#fff}
.region-favorite:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
.region-empty{display:flex;flex-direction:column;align-items:center;gap:12px;padding:40px 20px;color:#9c8bab;text-align:center;border:1px dashed #ded5eb;border-radius:14px}
.region-empty p{margin:0;font-size:14px;color:#756584}
.region-empty span,.favorite-notice{font-size:11px;line-height:1.7;color:#9c8bab}
.region-empty button{margin-top:6px}
.marksix-region-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px}
.marksix-region-grid .game-card{min-height:0;border-radius:14px;box-shadow:0 3px 12px #4f406706}
.marksix-region-grid .game-card:hover{transform:translateY(-2px);box-shadow:0 7px 18px #4f406712}
.marksix-region-grid .game-card-main{height:190px;padding:18px;text-align:left}
.marksix-region-grid .game-copy{position:relative;z-index:2;min-width:0}
.marksix-region-grid .game-copy small{font-size:8px;letter-spacing:1.1px}
.marksix-region-grid .game-copy h3{font-size:clamp(16px,1.5vw,20px);line-height:1.3;margin:7px 0 8px;white-space:normal}
.marksix-region-grid .game-copy p{font-size:11px;line-height:1.6;letter-spacing:0;margin:0}
.marksix-region-grid .game-copy .region-period{font-weight:600}
.marksix-region-grid .game-copy .region-format{margin-top:5px;font-size:10px}
.marksix-region-grid .game-enter{display:flex;gap:5px;margin-top:14px;font-size:10px}
.marksix-region-grid .game-enter svg{width:12px;height:12px}
.marksix-region-grid :deep(.game-art){transform:scale(.46);transform-origin:right bottom;right:0;bottom:8px;opacity:.35}
@media(max-width:1099px){.marksix-region-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:600px){
 .marksix-region-grid{grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}
 .marksix-region-grid .game-card{border-radius:12px}
 .marksix-region-grid .game-card-main{height:175px;padding:14px 12px}
 .marksix-region-grid .game-copy h3{font-size:clamp(15px,4vw,20px);margin:8px 0}
 .marksix-region-grid .game-enter{margin-top:13px}
 .marksix-region-grid :deep(.game-art){transform:scale(.36);right:0;bottom:8px}
}
</style>
