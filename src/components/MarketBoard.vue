<script setup lang="ts">
import {computed,ref,onMounted,onUnmounted,watch} from 'vue'
import {Check,RefreshCw,ChevronRight,Layers,Hash,X} from 'lucide-vue-next'
import {api,APIError,user,refreshAccount,money,key,type PlaySetting,type Order} from '../api/account'
import StakeAmount from './StakeAmount.vue'
import RacingMarketBoard from './RacingMarketBoard.vue'
import DiceMarketBoard from './DiceMarketBoard.vue'
import DiceBetConfirmation from './DiceBetConfirmation.vue'
import MarkSixMarketBoard from './MarkSixMarketBoard.vue'
import {parseStake} from '../lib/stake'
import {quoteID,usesZodiac} from '../lib/marksix'
import {selectionOdds} from '../lib/marksixOdds'
import {copyTierOdds,sameTierOdds,validTierOdds} from '../lib/marketOdds'
import {oddsText,type Market,type MarketBook,type MarketBatchInput} from '../api/markets'
const props=defineProps<{sessionId:string;playSetting?:PlaySetting;targetIssue:string;blocked:boolean;blockLabel?:string;compact?:boolean;racing?:boolean;dice?:boolean;initialAmount?:string}>()
const emit=defineEmits<{notify:[message:string];login:[];busy:[busy:boolean]}>()
const book=ref<MarketBook|null>(null),mode=ref<'two_sides'|'regular'>('two_sides'),position=ref(''),error=ref(''),loading=ref(true),busy=ref(false),amount=ref(props.initialAmount??'10')
const diceConfirmOpen=ref(false)
interface Selection {market:Market;odds:number;tierOdds?:Record<string,number>;unit:number}
const chosen=ref<Selection[]>([]),requestKey=ref(key()),retryPayload=ref<MarketBatchInput|null>(null)
const retryOwner=ref<{id:string;username:string}|null>(null)
const racingMode=ref<'quick'|'general'>('quick'),selectionAmounts=ref<Record<string,string>>({})
const locked=computed(()=>busy.value||!!retryPayload.value)
watch(locked,value=>emit('busy',value),{immediate:true,flush:'sync'})
let poll:ReturnType<typeof setInterval>|undefined,controller:AbortController|null=null,mounted=true
const quotes=computed(()=>Object.fromEntries((book.value?.quotes??[]).map(q=>[q.market_id,q])))
const marksix=computed(()=>!!book.value?.marksix_context)
const zodiacOutdated=computed(()=>marksix.value&&chosen.value.some(selection=>usesZodiac(selection.market)&&(book.value?.marksix_context?.issue!==props.targetIssue||selection.market.selection_context_issue!==props.targetIssue)))
const selectedIDs=computed(()=>new Set(chosen.value.map(c=>c.market.id)))
const positions=computed(()=>[...new Set(book.value?.markets.filter(m=>m.category==='regular').map(m=>m.group)??[])])
const visible=computed(()=>book.value?.markets.filter(m=>mode.value==='two_sides'?m.category==='two_sides':m.group===position.value)??[])
const groups=computed(()=>[...new Set(visible.value.map(m=>m.group))].map(group=>({name:group,rows:[visible.value.filter(m=>m.group===group&&m.kind!=='number'),visible.value.filter(m=>m.group===group&&m.kind==='number')].filter(row=>row.length)})))
const compactGroup=ref(''),chipsOpen=ref(false),selectionOpen=ref(false)
const compactGroups=computed(()=>[...new Set(book.value?.markets.map(m=>m.group)??[])])
const compactOptions=computed(()=>book.value?.markets.filter(m=>m.group===(compactGroup.value||compactGroups.value[0]))??[])
const cost=computed(()=>parseStake(amount.value))
const individualAmounts=computed(()=>!!props.racing&&racingMode.value==='general')
const selectionCost=(id:string)=>individualAmounts.value?parseStake(selectionAmounts.value[id]??''):cost.value
const total=computed(()=>chosen.value.reduce((sum,c)=>sum+selectionCost(c.market.id),0))
const actualOdds=(m:Market)=>selectionOdds(m,quotes.value[quoteID(m)],book.value?.marksix_context)
const changed=computed(()=>chosen.value.some(c=>actualOdds(c.market)!==c.odds||!sameTierOdds(quotes.value[quoteID(c.market)]?.tier_odds_milli,c.tierOdds)||props.playSetting?.unit_cents!==c.unit))
const unavailable=computed(()=>chosen.value.some(c=>!quotes.value[quoteID(c.market)]?.enabled||!validTierOdds(c.market,quotes.value[quoteID(c.market)]?.tier_odds_milli)))
const invalidCost=(value:number)=>value<(props.playSetting?.unit_cents??200)||value>(props.playSetting?.max_stake_cents??100000)
const invalidAmount=computed(()=>individualAmounts.value?chosen.value.some(c=>invalidCost(selectionCost(c.market.id))):invalidCost(cost.value))
const submitDisabled=computed(()=>busy.value||(!retryPayload.value&&(props.blocked||!chosen.value.length||changed.value||unavailable.value||zodiacOutdated.value||!!error.value||invalidAmount.value)))
const diceConfirmation=computed(()=>!!props.dice&&!props.racing&&!marksix.value)
const placeDisabled=computed(()=>diceConfirmation.value?busy.value||!chosen.value.length||props.blocked&&!retryPayload.value:submitDisabled.value)
const retryNotice=computed(()=>retryPayload.value?user.value?'投注结果待确认，请重试原订单':`投注结果待确认，请重新登录原账号 ${retryOwner.value?.username??''} 后重试原订单`:'')
const racingNotice=computed(()=>retryNotice.value||error.value||(zodiacOutdated.value?'生肖资料与当前期号不一致，请等待同步后重新选择':unavailable.value?'部分已选选项已暂停，请移除后再提交':changed.value?'赔率或起投金额已变更，请确认最新赔率':props.blocked?(props.blockLabel??'暂不可投注'):invalidAmount.value&&chosen.value.length?`每项金额须为 ${money(props.playSetting?.unit_cents??200)}–${money(props.playSetting?.max_stake_cents??100000)} 元`:''))
async function load() {
 const sessionId=props.sessionId
 if(!sessionId?.trim()) { loading.value=true; return }
 if(controller)return
 controller=new AbortController();const c=controller
 const timeout=setTimeout(()=>c.abort(),8000)
 try {
  const r=await fetch(`/api/v1/markets/${encodeURIComponent(sessionId)}`,{signal:c.signal,cache:'no-store'})
  const body=await r.json()
  if(!r.ok)throw new Error(r.status===404?'该场次赔率暂不可用，请刷新页面后重试':body.error?.message??'赔率读取失败')
  if(!mounted||c.signal.aborted||props.sessionId!==sessionId)return
  if(body.data?.session_id!==sessionId||!Array.isArray(body.data.markets)||!Array.isArray(body.data.quotes))throw new Error('赔率数据异常，请重试')
  book.value=body.data;if(!position.value)position.value=positions.value[0]??'';error.value=''
 } catch(e) {
  if(mounted&&!c.signal.aborted&&props.sessionId===sessionId)error.value=(e as Error).message
  else if(mounted&&controller===c&&props.sessionId===sessionId)error.value='赔率读取超时，请重试'
 } finally {
  clearTimeout(timeout)
  if(controller===c){controller=null;loading.value=false}
 }
}

function resetSelection(){diceConfirmOpen.value=false;chosen.value=[];selectionAmounts.value={};retryPayload.value=null;retryOwner.value=null;requestKey.value=key()}
watch(()=>chosen.value.length,count=>{if(!count)diceConfirmOpen.value=false},{flush:'sync'})
watch(()=>props.sessionId,()=>{
 controller?.abort();controller=null;book.value=null;resetSelection();position.value='';compactGroup.value='';error.value='';loading.value=true
 void load()
},{immediate:true})
watch(()=>props.targetIssue,(next,previous)=>{
 if(next!==previous&&!locked.value&&chosen.value.length){resetSelection();emit('notify','期号已更新，请重新选择本期投注')}
})
watch(()=>user.value?.id,(next,previous)=>{
 if(previous&&next!==previous)diceConfirmOpen.value=false
 if(retryPayload.value){
  if(!next){emit('notify',retryNotice.value);return}
  if(next!==retryOwner.value?.id){
   const owner=retryOwner.value?.username??'原账号'
   resetSelection()
   emit('notify',`已切换账号，未向当前账号提交原订单；${owner} 的投注结果仍待确认，请登录该账号查看订单记录`)
  }
  return
 }
 if(previous&&next!==previous)resetSelection()
},{flush:'sync'})
onMounted(()=>{poll=setInterval(()=>void load(),4000)})
onUnmounted(()=>{mounted=false;controller?.abort();clearInterval(poll);emit('busy',false)})
watch(amount,()=>{if(!locked.value)requestKey.value=key()},{flush:'sync'})
function remove(id:string){if(locked.value)return;chosen.value=chosen.value.filter(c=>c.market.id!==id);selectionAmounts.value[id]='';requestKey.value=key()}
function select(m:Market){
 if(locked.value)return
 if(selectedIDs.value.has(m.id)){remove(m.id);return}
 const q=quotes.value[quoteID(m)];if(!q?.enabled)return
 if(!validTierOdds(m,q.tier_odds_milli)){emit('notify','该玩法分档赔率尚未就绪，请刷新后重试');return}
 if(chosen.value.length>=500){emit('notify','每次最多提交500注，请减少选择');return}
 if(marksix.value&&usesZodiac(m)){
  if(book.value?.marksix_context?.issue!==props.targetIssue){emit('notify','生肖资料正在同步，请稍后再选');return}
  m={...m,selection_context_issue:props.targetIssue}
 }
 if(individualAmounts.value&&!parseStake(selectionAmounts.value[m.id]??''))selectionAmounts.value[m.id]=invalidCost(cost.value)?String((props.playSetting?.unit_cents??200)/100):amount.value
 chosen.value.push({market:m,odds:actualOdds(m),tierOdds:copyTierOdds(q.tier_odds_milli),unit:props.playSetting?.unit_cents??200});requestKey.value=key()
}
function addMarkets(markets:Market[]){
 if(locked.value)return
 const unique=new Map(markets.filter(market=>!selectedIDs.value.has(market.id)).map(market=>[market.id,market]))
 if(chosen.value.length+unique.size>500){emit('notify','每次最多提交500注，请减少选择');return}
 if([...unique.values()].some(market=>!quotes.value[quoteID(market)]?.enabled||!validTierOdds(market,quotes.value[quoteID(market)]?.tier_odds_milli)||usesZodiac(market)&&book.value?.marksix_context?.issue!==props.targetIssue)){emit('notify','当前玩法或生肖资料不可用，请刷新后重选');return}
 for(const market of unique.values())chosen.value.push({market,odds:actualOdds(market),tierOdds:copyTierOdds(quotes.value[quoteID(market)]!.tier_odds_milli),unit:props.playSetting?.unit_cents??200})
 if(unique.size){requestKey.value=key();emit('notify',`已加入${unique.size}注，共${chosen.value.length}注`)}
}
function setRacingMode(next:'quick'|'general'){
 if(locked.value)return
 racingMode.value=next
 if(next==='general')for(const c of chosen.value)if(!parseStake(selectionAmounts.value[c.market.id]??''))selectionAmounts.value[c.market.id]=invalidCost(cost.value)?String((props.playSetting?.unit_cents??200)/100):amount.value
 requestKey.value=key()
}
function setSelectionAmount(m:Market,value:string){
 if(locked.value||!quotes.value[m.id]?.enabled)return
 selectionAmounts.value[m.id]=value
 if(parseStake(value)>0){if(!selectedIDs.value.has(m.id))select(m)}
 else if(!value.trim())chosen.value=chosen.value.filter(c=>c.market.id!==m.id)
 requestKey.value=key()
}
function confirmQuotes(){
 if(locked.value)return
 chosen.value=chosen.value.map(c=>quotes.value[quoteID(c.market)]?.enabled?{...c,odds:actualOdds(c.market),tierOdds:copyTierOdds(quotes.value[quoteID(c.market)]!.tier_odds_milli),unit:props.playSetting?.unit_cents??200}:c)
 requestKey.value=key()
}
function requestSubmit(){
 if(!diceConfirmation.value){void submit();return}
 if(busy.value||!chosen.value.length)return
 chipsOpen.value=false;selectionOpen.value=false;diceConfirmOpen.value=true
}
function confirmDiceSubmit(){if(!diceConfirmOpen.value||busy.value)return;if(!user.value)diceConfirmOpen.value=false;void submit()}
async function submit(){
 if(busy.value||!chosen.value.length)return
 if(!user.value){emit('login');return}
 if(retryPayload.value&&retryOwner.value?.id!==user.value.id){emit('notify','原订单属于其他账号，请登录原账号确认结果');return}
 if(!retryPayload.value){
  if(props.blocked||changed.value||unavailable.value||zodiacOutdated.value||error.value){emit('notify','当前赔率或期号不可用，请确认后再提交');return}
  if(invalidAmount.value){emit('notify','请输入本场允许的每注金额');return}
  if(user.value.balance_cents<total.value){emit('notify',`余额不足，本次${chosen.value.length}注共需${money(total.value)}元`);return}
  retryOwner.value={id:user.value.id,username:user.value.username}
  retryPayload.value={session_id:props.sessionId,issue:props.targetIssue,unit_cents:props.playSetting?.unit_cents??200,amount_cents:individualAmounts.value&&invalidCost(cost.value)?(props.playSetting?.unit_cents??200):cost.value,...(individualAmounts.value?{selection_amounts:Object.fromEntries(chosen.value.map(c=>[c.market.id,selectionCost(c.market.id)]))}:{}),selections:chosen.value.map(c=>({market_id:quoteID(c.market),odds_milli:c.odds,...(c.tierOdds?{tier_odds_milli:copyTierOdds(c.tierOdds)}:{}),...(c.market.selection_values?{values:[...c.market.selection_values]}:{})})),key:requestKey.value}
 }
 const payload=retryPayload.value,owner=retryOwner.value!.id
 const isCurrentAttempt=()=>retryPayload.value===payload&&retryOwner.value?.id===owner
 busy.value=true
 try{
  const result=await api<{orders:Order[];total_cost_cents:number}>('/market-orders/batch','POST',payload)
  if(!isCurrentAttempt())return
  resetSelection()
  const message=`投注成功，共${result.orders.length}注，合计${money(result.total_cost_cents)}元`
  if(user.value?.id!==owner){emit('notify',message);return}
  try{await refreshAccount();emit('notify',message)}catch{emit('notify',message+'；余额刷新失败，请刷新账户')}
 }catch(e){
  if(!isCurrentAttempt())return
  // An uncertain response must retry the exact batch, even if the issue has rolled.
  // Keep selections locked so a lost response cannot turn into another debit.
  if(e instanceof APIError&&[400,404,409,422].includes(e.status)){
   retryPayload.value=null;retryOwner.value=null;requestKey.value=key()
   if(props.targetIssue!==payload.issue)resetSelection()
   emit('notify',e.message)
  }else{
   emit('notify',user.value?'投注结果尚未确认，请点击“重试确认”，原订单不会重复扣款':retryNotice.value)
   if(e instanceof APIError&&e.status===401)emit('login')
  }
 }finally{busy.value=false}
}
</script>
<template><section class="market-board" :class="{'compact-market':compact,'racing-market':racing,'dice-market':dice,'marksix-market':marksix}"><div v-if="!racing && !marksix && !dice && !compact" class="market-toolbar"><div class="market-mode" role="tablist" aria-label="玩法分类"><button role="tab" :aria-selected="mode==='two_sides'" :class="{active:mode==='two_sides'}" @click="mode='two_sides'"><Layers :size="17"/>两面</button><button role="tab" :aria-selected="mode==='regular'" :class="{active:mode==='regular'}" @click="mode='regular'"><Hash :size="17"/>常规</button></div><button class="text-button" @click="load"><RefreshCw :size="14"/>刷新赔率</button></div>
<div v-if="error" class="form-error" role="alert">{{error}}<button @click="load">重试</button></div><div v-if="loading&&!book" class="room-loading">正在读取场次赔率…</div>
<MarkSixMarketBoard v-if="marksix && book" :book="book" :selections="chosen" :amount="amount" :locked="locked" :busy="busy" :blocked="blocked" :retrying="!!retryPayload" :issue="retryPayload?.issue??targetIssue" :target-issue="targetIssue" :min="playSetting?.unit_cents??200" :max="playSetting?.max_stake_cents??100000" :total="total" :submit-disabled="submitDisabled" :notice="racingNotice" :can-confirm-quotes="changed&&!unavailable" @select="select" @add="addMarkets" @remove="remove" @clear="resetSelection" @amount="amount=$event" @submit="submit" @confirm-quotes="confirmQuotes" @refresh="load" @notify="emit('notify',$event)"/>
<RacingMarketBoard v-if="racing && book" :book="book" :selections="chosen" :entry-mode="racingMode" :amount="amount" :selection-amounts="selectionAmounts" :locked="locked" :busy="busy" :blocked="blocked" :retrying="!!retryPayload" :issue="retryPayload?.issue??targetIssue" :min="playSetting?.unit_cents??200" :max="playSetting?.max_stake_cents??100000" :total="total" :submit-disabled="submitDisabled" :notice="racingNotice" :can-confirm-quotes="changed&&!unavailable" @select="select" @remove="remove" @clear="resetSelection" @mode="setRacingMode" @amount="amount=$event" @selection-amount="setSelectionAmount" @submit="submit" @confirm-quotes="confirmQuotes" @refresh="load" @login="emit('login')" @notify="emit('notify',$event)"/>
<DiceMarketBoard v-if="dice && !racing && !marksix && book" :book="book" :selections="chosen" :locked="locked" @select="select" @refresh="load"/>
<div v-if="!racing && !marksix && !dice && compact && book" class="compact-market-layout">
 <nav class="compact-play-menu" aria-label="选择玩法"><button v-for="group in compactGroups" :key="group" :class="{active:(compactGroup||compactGroups[0])===group}" :aria-pressed="(compactGroup||compactGroups[0])===group" @click="compactGroup=group">{{group}}<ChevronRight :size="13"/></button></nav>
 <div class="compact-play-content"><header><span>{{compactGroup||compactGroups[0]}}</span><button aria-label="刷新赔率" @click="load"><RefreshCw :size="14"/></button></header><div class="compact-play-options" :class="{'has-numbers':compactOptions.some(m=>m.kind==='number')}"><button v-for="m in compactOptions" :key="m.id" :disabled="locked||!quotes[m.id]?.enabled" :class="{selected:selectedIDs.has(m.id),numeric:m.kind==='number'}" :aria-label="`${m.group}${m.label}，赔率${oddsText(quotes[m.id]?.odds_milli??0)}`" :aria-pressed="selectedIDs.has(m.id)" @click="select(m)"><strong>{{m.label}}</strong><span>{{quotes[m.id]?.enabled?oddsText(quotes[m.id]!.odds_milli):'已暂停'}}</span><Check v-if="selectedIDs.has(m.id)" :size="12"/></button></div></div>
</div>
<div v-if="!racing && !marksix && !dice && !compact && book" class="market-layout" :class="{regular:mode==='regular'}"><nav v-if="mode==='regular'" class="position-tabs" aria-label="选择投注位置"><button v-for="p in positions" :key="p" :class="{active:position===p}" @click="position=p">{{p}}<ChevronRight :size="14"/></button></nav><div class="market-groups"><section v-for="g in groups" :key="g.name" class="market-group"><div class="market-group-title"><h3>{{g.name}}</h3><small>{{mode==='two_sides'?'两面玩法':'定位大小单双 · 指定数字'}}</small></div><div v-for="(row,rowIndex) in g.rows" :key="rowIndex" class="market-options" :style="row[0]?.kind==='compare'?{gridTemplateColumns:`repeat(${row.length},minmax(0,1fr))`}:undefined" :class="{'market-numbers':row[0]?.kind==='number','market-compare':row[0]?.kind==='compare'}"><button v-for="m in row" :key="m.id" :disabled="locked||!quotes[m.id]?.enabled" :class="{selected:selectedIDs.has(m.id),numeric:m.kind==='number'}" :aria-label="`${g.name}${m.label}，赔率${oddsText(quotes[m.id]?.odds_milli??0)}`" :aria-pressed="selectedIDs.has(m.id)" @click="select(m)"><strong>{{m.label}}</strong><span>{{quotes[m.id]?.enabled?`赔率 ${oddsText(quotes[m.id]!.odds_milli)}`:'已暂停'}}</span><Check v-if="selectedIDs.has(m.id)" :size="13" class="market-selected"/></button></div></section></div></div>
<div v-if="!racing && !marksix && !compact" class="market-selection"><div><small>当前选择 · {{chosen.length}} 注</small><strong>{{chosen.length?'可继续多选，每个选项单独成注':'可同时选择多个投注选项'}}</strong></div><button v-if="chosen.length" class="text-button" :disabled="locked" @click="resetSelection">清空全部</button></div>
<div v-if="!racing && !marksix && chosen.length && (!compact || selectionOpen)" class="market-selected-list" :class="{'compact-selection-drawer':compact}" aria-label="已选投注">
 <article v-for="c in chosen" :key="c.market.id"><div><strong>{{c.market.group}} · {{c.market.label}}</strong><small>赔率 {{oddsText(c.odds)}} · 单注中奖返还 {{money(Math.floor(cost*c.odds/1000))}} 元</small></div><button class="icon-button" :disabled="locked" :aria-label="`移除${c.market.group}${c.market.label}`" @click="remove(c.market.id)"><X :size="15"/></button></article>
</div>
<p v-if="!racing && !marksix && (changed||unavailable)" class="form-error">{{unavailable?'部分已选选项已暂停，请移除后再提交。':'所选赔率或单注金额已变更，请确认最新赔率。'}} <button v-if="changed&&!unavailable" class="text-button" :disabled="locked" @click="confirmQuotes">确认最新赔率</button></p>
<p v-if="!racing && !marksix && retryPayload&&!busy" class="form-error" role="alert">{{retryNotice}}。原 {{retryPayload.selections.length}} 注已保留，确认前暂不能修改选择或金额。</p>
<div v-if="!racing && !marksix && (!compact || chipsOpen)" :class="{'compact-chip-drawer':compact}"><button v-if="compact" class="compact-drawer-close" @click="chipsOpen=false"><X :size="16"/>关闭筹码</button><StakeAmount v-model="amount" label="每注金额" :min="playSetting?.unit_cents??200" :max="playSetting?.max_stake_cents??100000" :disabled="locked"/></div>
<div v-if="!racing && !marksix && !compact" class="market-submit"><div class="market-totals"><span>总投注金额 <b>{{money(total)}}</b></span><small>{{chosen.length}} 注 × 每注 {{money(cost)}} 元 · 分别结算</small></div><button class="primary-button" :disabled="placeDisabled" @click="requestSubmit">{{busy?'提交中…':retryPayload?'重试确认':blocked?(blockLabel??'暂不可投注'):!user?'登录后投注':`确认投注（${chosen.length}注）`}}<ChevronRight :size="17"/></button></div>
<div v-if="!racing && !marksix && compact" class="compact-bet-dock">
 <div class="compact-bet-input"><button :aria-expanded="selectionOpen" @click="diceConfirmation?requestSubmit():(selectionOpen=!selectionOpen, chipsOpen=false)">已选中 {{chosen.length}} 注 <small>共 {{money(total)}} 元</small></button><input v-model="amount" :disabled="locked" inputmode="decimal" aria-label="每注金额（元）" placeholder="请输入每注金额"/></div>
 <button class="compact-place" :disabled="placeDisabled" @click="requestSubmit">{{busy?'提交中':retryPayload?'重试确认':blocked?'已封盘':!user?'登录':'下注'}}</button>
 <button class="compact-chips" :aria-expanded="chipsOpen" @click="chipsOpen=!chipsOpen;selectionOpen=false">筹码</button>
 <button class="compact-reset" :disabled="locked" @click="resetSelection">重置</button>
 <p v-if="blocked||invalidAmount||changed||unavailable||retryPayload||error" class="compact-bet-hint" role="status">{{retryPayload?retryNotice:error?error:changed?'赔率已变更，请确认最新赔率':unavailable?'已选选项暂停，请移除':blocked?(blockLabel||'暂不可投注'):`每注金额范围 ${money(playSetting?.unit_cents??200)}–${money(playSetting?.max_stake_cents??100000)} 元`}}<button v-if="changed&&!unavailable" :disabled="locked" @click="confirmQuotes">确认最新赔率</button></p>
</div>
<DiceBetConfirmation v-if="diceConfirmOpen&&diceConfirmation" :selections="chosen" :issue="retryPayload?.issue??targetIssue" :amount="amount" :min="playSetting?.unit_cents??200" :max="playSetting?.max_stake_cents??100000" :total="total" :locked="locked" :busy="busy" :retrying="!!retryPayload" :logged-in="!!user" :submit-disabled="submitDisabled" :notice="racingNotice" :can-confirm-quotes="changed&&!unavailable" @close="!busy&&(diceConfirmOpen=false)" @amount="amount=$event" @remove="remove" @submit="confirmDiceSubmit" @confirm-quotes="confirmQuotes"/>
</section></template>
<style scoped>
.market-selected-list{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;max-height:210px;overflow:auto;margin:0 24px 16px;padding:2px}
.market-selected-list article{display:flex;align-items:center;justify-content:space-between;gap:8px;padding:10px 12px;border:1px solid #e7e0f5;border-radius:9px;background:#faf8ff;min-width:0}
.market-selected-list article>div{min-width:0}
.market-selected-list strong{display:block;color:#655386;font-size:12px;overflow-wrap:anywhere}
.market-selected-list small{display:block;color:#978aa9;font-size:10px;line-height:1.6;margin-top:4px}
.market-selected-list .icon-button{flex-shrink:0;width:28px;height:28px}
.market-selected-list button:disabled{opacity:.5;cursor:not-allowed}
@media(max-width:640px){.market-selected-list{grid-template-columns:1fr;margin:0 14px 12px;max-height:220px}}
</style>

<style scoped>
.compact-market{border-radius:0;margin:0;border:0;overflow:visible;padding-bottom:110px;background:#f5f5f5}
.compact-market-layout{display:grid;grid-template-columns:125px minmax(0,1fr);min-height:500px;background:#fff}
.compact-play-menu{background:#f4f4f4;border-right:1px solid #dcdcdc;align-self:stretch}
.compact-play-menu button{width:100%;display:flex;align-items:center;justify-content:space-between;gap:4px;padding:17px 12px;background:#fff;border-bottom:1px solid #e8e8e8;color:#37343e;font-size:14px;min-height:54px}
.compact-play-menu button.active{background:linear-gradient(110deg,#4b51e7,#9160ed);color:#fff}
.compact-play-content{min-width:0}.compact-play-content>header{height:44px;display:flex;align-items:center;justify-content:space-between;padding:0 15px;background:#f6f4f3;border-bottom:1px solid #e6e3e3;font-size:14px;color:#a15e63}
.compact-play-content>header button{width:32px;height:32px;display:grid;place-items:center}
.compact-play-options{display:grid;grid-template-columns:repeat(2,minmax(0,1fr))}
.compact-play-options.has-numbers{grid-template-columns:repeat(4,minmax(0,1fr))}
.compact-play-options button{position:relative;min-width:0;min-height:64px;padding:12px 4px;display:flex;align-items:center;justify-content:center;gap:12px;border-right:1px solid #e6e6e6;border-bottom:1px solid #e6e6e6;border-radius:0;background:#fff;color:#24212c}
.compact-play-options strong{font-size:21px;font-weight:600}.compact-play-options span{font-size:13px;font-variant-numeric:tabular-nums}
.compact-play-options .numeric strong{font-size:17px;display:grid;place-items:center;background:linear-gradient(135deg,#a08bea,#6f58d2);color:#fff;width:30px;height:30px;border-radius:50%;box-shadow:inset 0 0 0 3px #ffffff5a}
.compact-play-options button.selected{background:#f0e9ff;box-shadow:inset 0 0 0 2px #9170df;color:#7043bb}.compact-play-options button>svg{position:absolute;right:4px;top:4px}.compact-play-options button:disabled{opacity:.4}
.compact-bet-dock{position:fixed;z-index:35;bottom:0;left:232px;right:0;display:flex;align-items:stretch;gap:8px;background:#676767;color:#fff;padding:9px 15px calc(9px + env(safe-area-inset-bottom,0px));box-shadow:0 -2px 9px #0001;flex-wrap:wrap}
.compact-bet-input{flex:1;min-width:80px}.compact-bet-input>button{font-size:15px;font-weight:600;padding:0 0 6px;text-align:left;display:block}.compact-bet-input small{font-size:11px;font-weight:400;margin-left:8px;white-space:nowrap}.compact-bet-input input{width:100%;display:block;height:34px;border:0;border-radius:4px;padding:7px 10px;font-size:14px;color:#464051;background:#fff}
.compact-bet-dock>button{width:80px;min-height:58px;border-radius:4px;font-size:15px;font-weight:600;padding:5px}.compact-place{background:#97bf70}.compact-chips{background:#dc7856}.compact-reset{background:#5c8bd2}.compact-bet-hint button{margin-left:8px;text-decoration:underline;color:#fff}.compact-bet-hint{width:100%;font-size:11px;color:#ffdfb1;line-height:1.4}
.compact-chip-drawer,.compact-selection-drawer{position:fixed;left:232px;right:0;bottom:108px;z-index:36;max-height:50dvh;overflow:auto;box-shadow:0 -4px 16px #29212e2b;background:#fff;padding:10px;border-radius:10px 10px 0 0;margin:0}
.compact-selection-drawer{grid-template-columns:repeat(3,minmax(0,1fr))}.compact-drawer-close{display:flex;gap:5px;align-items:center;margin-left:auto;font-size:12px;color:#8d73a6;padding:4px 8px}.compact-chip-drawer :deep(.stake-control){border:0;padding:12px}
@media(max-width:1000px) and (min-width:761px){.compact-bet-dock,.compact-chip-drawer,.compact-selection-drawer{left:176px}}
@media(max-width:760px){.compact-market-layout{grid-template-columns:85px minmax(0,1fr);min-height:calc(100dvh - 215px)}.compact-play-menu button{padding:14px 9px;font-size:12px;min-height:47px}.compact-play-content>header{height:35px;padding:0 10px;font-size:12px}.compact-play-options.has-numbers{grid-template-columns:repeat(2,minmax(0,1fr))}.compact-play-options button{min-height:54px;gap:8px;padding:8px 3px}.compact-play-options strong{font-size:19px}.compact-play-options span{font-size:12px}.compact-play-options .numeric strong{font-size:15px;width:27px;height:27px}.compact-bet-dock{left:0;padding:8px 8px calc(8px + env(safe-area-inset-bottom,0px));gap:7px}.compact-bet-dock>button{width:51px;min-height:56px;font-size:13px}.compact-bet-input>button{font-size:13px;padding-bottom:6px}.compact-bet-input small{display:block;margin:0;font-size:9px;line-height:1.4}.compact-bet-input input{height:30px;font-size:13px;padding:6px}.compact-bet-hint{font-size:10px}.compact-chip-drawer,.compact-selection-drawer{left:0;bottom:112px;max-height:45dvh;padding:8px}.compact-selection-drawer{grid-template-columns:1fr}.compact-market{padding-bottom:125px}}
</style>

<style scoped>
.racing-market.compact-market,.marksix-market.compact-market{display:flex;flex-direction:column;flex:1;min-height:0;overflow:hidden;padding-bottom:0;background:#fff}
.racing-market>.form-error,.racing-market>.room-loading,.marksix-market>.form-error,.marksix-market>.room-loading{flex-shrink:0}
</style>
