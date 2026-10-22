<script setup lang="ts">
import {computed,ref} from 'vue'
import {Check,ChevronRight,RefreshCw} from 'lucide-vue-next'
import {oddsText,type Market,type MarketBook} from '../api/markets'
import DiceFace from './DiceFace.vue'

const props=defineProps<{book:MarketBook;selections:{market:Market;odds:number}[];locked:boolean}>()
const emit=defineEmits<{select:[market:Market];refresh:[]}>()
const content=ref<HTMLElement|null>(null),active=ref('sides')
const quotes=computed(()=>Object.fromEntries(props.book.quotes.map(q=>[q.market_id,q])))
const selectedIDs=computed(()=>new Set(props.selections.map(selection=>selection.market.id)))
const sections=computed(()=>[
 {id:'sides',menu:'骰宝',title:'骰宝 / 大小',groups:['三军','双面']},
 {id:'triples',menu:'围骰',title:'围骰 / 全骰',groups:['围骰','全骰']},
 {id:'sums',menu:'点数',title:'点数',groups:['点数']},
].map(section=>({...section,markets:section.groups.flatMap(group=>props.book.markets.filter(market=>market.group===group))})).filter(section=>section.markets.length))
function faces(market:Market){
 const single=market.id.match(/\.single\.n([1-6])$/)
 if(single)return [Number(single[1])]
 const triple=market.id.match(/\.triple\.n([1-6])$/)
 if(triple)return Array<number>(3).fill(Number(triple[1]))
 return []
}
function scrollToSection(id:string){
 active.value=id
 content.value?.querySelector<HTMLElement>(`[data-section="${id}"]`)?.scrollIntoView({behavior:'smooth',block:'start'})
}
</script>

<template>
 <div class="dice-board">
  <nav class="dice-menu" aria-label="骰宝玩法导航">
   <button v-for="section in sections" :key="section.id" :class="{active:active===section.id}" :aria-current="active===section.id?'location':undefined" @click="scrollToSection(section.id)">{{section.menu}}<ChevronRight :size="12"/></button>
  </nav>
  <div ref="content" class="dice-content">
   <section v-for="section in sections" :key="section.id" :data-section="section.id" class="dice-section">
    <header><h3>{{section.title}}</h3><button v-if="section.id===sections[0]?.id" class="dice-refresh" :disabled="locked" aria-label="刷新骰宝赔率" @click="emit('refresh')"><RefreshCw :size="14"/></button></header>
    <div class="dice-options">
     <button v-for="market in section.markets" :key="market.id" class="dice-option" :class="{selected:selectedIDs.has(market.id),'any-triple':market.group==='全骰','triple-option':market.group==='围骰'}" :disabled="locked||!quotes[market.id]?.enabled" :aria-label="`${market.group} ${market.label}，赔率${oddsText(quotes[market.id]?.odds_milli??0)}`" :aria-pressed="selectedIDs.has(market.id)" @click="emit('select',market)">
      <span v-if="faces(market).length" class="dice-faces" :class="{'triple-faces':faces(market).length===3}" aria-hidden="true"><DiceFace v-for="(face,index) in faces(market)" :key="index" :number="face"/></span>
      <strong v-else :class="{'dice-sum-number':market.group==='点数'}">{{market.group==='全骰'?'全骰':market.label}}<small v-if="market.group==='全骰'">任意三同号</small></strong>
      <span class="dice-odds">{{quotes[market.id]?.enabled?oddsText(quotes[market.id]!.odds_milli):'已暂停'}}</span>
      <Check v-if="selectedIDs.has(market.id)" class="dice-check" :size="12"/>
     </button>
    </div>
   </section>
   <p v-if="!sections.length" class="dice-empty">暂无可用骰宝玩法</p>
  </div>
 </div>
</template>

<style scoped>
.dice-board{display:grid;grid-template-columns:110px minmax(0,1fr);align-items:stretch;min-height:500px;background:#fff;color:#29292d;font-family:Arial,'Microsoft YaHei','PingFang SC',sans-serif}
.dice-menu{background:#f4f4f5;border-right:1px solid #dedce2}.dice-menu button{display:flex;align-items:center;justify-content:space-between;gap:3px;width:100%;min-height:60px;padding:14px 15px;background:#fafafa;border-bottom:1px solid #e4e2e7;font-size:18px}.dice-menu button.active{background:linear-gradient(105deg,#4d63e5,#9c62e9);color:#fff}
.dice-content{min-width:0}
.dice-refresh{position:absolute;right:8px;top:50%;transform:translateY(-50%);display:grid;place-items:center;width:26px;height:26px;padding:0;color:#9480ac}.dice-refresh:disabled{opacity:.45;cursor:not-allowed}.dice-section{scroll-margin-top:12px}.dice-section>header{position:relative;display:flex;align-items:center;justify-content:center;min-height:44px;padding:9px 38px;text-align:center;background:#f8f6f6;border-bottom:1px solid #dfdde2}.dice-section h3{font-size:18px;font-weight:500;color:#a56e78;margin:0}
.dice-options{display:grid;grid-template-columns:repeat(2,minmax(0,1fr))}.dice-option{position:relative;display:flex;align-items:center;justify-content:center;gap:24px;min-width:0;min-height:66px;padding:12px;background:#fff;border-right:1px solid #dfdde2;border-bottom:1px solid #dfdde2;border-radius:0}.dice-option:nth-child(even){border-right:0}.dice-option strong{display:flex;align-items:center;gap:10px;justify-content:center;min-width:30px;font-size:24px;line-height:1.3;font-weight:500;color:#28282b}.dice-option strong small{font-size:13px;font-weight:400;color:#8b7d93}.dice-option strong.dice-sum-number{font-size:23px;color:#29282e}
.dice-faces{--dice-face-size:34px;display:flex;align-items:center;justify-content:center;gap:4px;flex-shrink:0}.dice-faces.triple-faces{--dice-face-size:27px;gap:3px}.dice-odds{font-size:20px;line-height:1.3;color:#343137;font-variant-numeric:tabular-nums;white-space:nowrap;flex-shrink:0}
.dice-option.selected{background:#f0e8fd;box-shadow:inset 0 0 0 1.5px #9b75d8}.dice-option.selected strong,.dice-option.selected .dice-odds{color:#8252c1}.dice-check{position:absolute;top:4px;right:4px;color:#8859c6}.dice-option.any-triple{grid-column:1/-1;border-right:0;gap:30px}.dice-option:disabled{opacity:.45;cursor:not-allowed}.dice-empty{padding:35px 15px;font-size:14px;color:#a294ad;text-align:center}
@media(max-width:760px){.dice-board{grid-template-columns:23.51% minmax(0,1fr);min-height:calc(100dvh - 215px)}.dice-menu button{min-height:52px;padding:12px 9px;font-size:16px}.dice-menu button>svg{width:12px}.dice-section>header{padding:8px 32px;min-height:39px}.dice-section h3{font-size:16px}.dice-option{min-height:61px;gap:12px;padding:10px 6px}.dice-option strong{min-width:25px;font-size:22px;gap:8px}.dice-option strong.dice-sum-number{font-size:21px}.dice-option strong small{font-size:12px}.dice-faces{--dice-face-size:29px;gap:3px}.dice-faces.triple-faces{--dice-face-size:22px;gap:3px}.dice-option.triple-option{gap:8px;padding-left:4px;padding-right:4px}.dice-odds{font-size:17px}.dice-option.any-triple{gap:25px}}
@media(max-width:360px){.dice-menu button{padding:11px 6px;font-size:15px}.dice-option{gap:7px;padding:10px 3px}.dice-faces{--dice-face-size:27px}.dice-faces.triple-faces{--dice-face-size:20px;gap:2px}.dice-option.triple-option{gap:6px;padding-left:3px;padding-right:3px}.dice-odds{font-size:15px}.dice-section h3{font-size:15px}}
</style>
