<script setup lang="ts">
import {computed,ref,onMounted,onUnmounted,watch} from 'vue'
import {Check,RefreshCw,ChevronRight,Layers,Hash} from 'lucide-vue-next'
import {api,APIError,user,refreshAccount,money,key,type PlaySetting} from '../api/account'
import StakeAmount from './StakeAmount.vue'
import {parseStake} from '../lib/stake'
import {oddsText,type Market,type MarketBook} from '../api/markets'
const props=defineProps<{sessionId:string;playSetting?:PlaySetting;targetIssue:string;blocked:boolean;blockLabel?:string}>()
const emit=defineEmits<{notify:[message:string];login:[];busy:[busy:boolean]}>()
const book=ref<MarketBook|null>(null),mode=ref<'two_sides'|'regular'>('two_sides'),position=ref(''),error=ref(''),loading=ref(true),busy=ref(false),amount=ref('10')
const chosen=ref<{market:Market;odds:number;unit:number;key:string}|null>(null)
let poll:ReturnType<typeof setInterval>|undefined,controller:AbortController|null=null,mounted=true
const quotes=computed(()=>Object.fromEntries((book.value?.quotes??[]).map(q=>[q.market_id,q])))
const positions=computed(()=>[...new Set(book.value?.markets.filter(m=>m.category==='regular').map(m=>m.group)??[])])
const visible=computed(()=>book.value?.markets.filter(m=>mode.value==='two_sides'?m.category==='two_sides':m.group===position.value)??[])
const groups=computed(()=>[...new Set(visible.value.map(m=>m.group))].map(group=>({name:group,rows:[visible.value.filter(m=>m.group===group&&m.kind!=='number'),visible.value.filter(m=>m.group===group&&m.kind==='number')].filter(row=>row.length)})))
const cost=computed(()=>parseStake(amount.value))
const expected=computed(()=>Math.floor(cost.value*(chosen.value?.odds??0)/1000))
const changed=computed(()=>!!chosen.value&&(quotes.value[chosen.value.market.id]?.odds_milli!==chosen.value.odds||props.playSetting?.unit_cents!==chosen.value.unit))
const unavailable=computed(()=>!!chosen.value&&!quotes.value[chosen.value.market.id]?.enabled)
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
watch(()=>props.sessionId,()=>{
 controller?.abort();controller=null;book.value=null;chosen.value=null;position.value='';error.value='';loading.value=true
 void load()
},{immediate:true})
onMounted(()=>{poll=setInterval(()=>void load(),4000)});onUnmounted(()=>{mounted=false;controller?.abort();clearInterval(poll)})
watch(amount,()=>{if(chosen.value&&!busy.value)chosen.value={...chosen.value,key:key()}})
function select(m:Market){if(busy.value)return;const q=quotes.value[m.id];if(!q?.enabled)return;if(chosen.value?.market.id===m.id&&!changed.value){chosen.value=null;return};chosen.value={market:m,odds:q.odds_milli,unit:props.playSetting?.unit_cents??200,key:key()}}
let retryPayload:unknown=null
async function submit(){if(!chosen.value||busy.value)return;if(!user.value){emit('login');return};if(props.blocked||changed.value||unavailable.value||error.value){emit('notify','当前赔率或期号不可用，请刷新后重新选择');return};if(cost.value<(props.playSetting?.unit_cents??200)||cost.value>(props.playSetting?.max_stake_cents??100000)){emit('notify','请输入本场允许的投注金额');return};if(user.value.balance_cents<cost.value){emit('notify','余额不足');return}
 busy.value=true;emit('busy',true)
 const payload=retryPayload??{session_id:props.sessionId,issue:props.targetIssue,market_id:chosen.value.market.id,odds_milli:chosen.value.odds,unit_cents:chosen.value.unit,amount_cents:cost.value,key:chosen.value.key};retryPayload=payload
 try{await api('/market-orders','POST',payload);retryPayload=null;chosen.value=null;await refreshAccount();emit('notify','投注成功')}
 catch(e){if(e instanceof APIError)retryPayload=null;emit('notify',(e as Error).message)}finally{busy.value=false;emit('busy',false)}
}
watch([()=>chosen.value?.key,amount],()=>{if(!busy.value)retryPayload=null})
</script>
<template><section class="market-board"><div class="market-toolbar"><div class="market-mode" role="tablist" aria-label="玩法分类"><button role="tab" :aria-selected="mode==='two_sides'" :class="{active:mode==='two_sides'}" @click="mode='two_sides'"><Layers :size="17"/>两面</button><button role="tab" :aria-selected="mode==='regular'" :class="{active:mode==='regular'}" @click="mode='regular'"><Hash :size="17"/>常规</button></div><button class="text-button" @click="load"><RefreshCw :size="14"/>刷新赔率</button></div>
<div v-if="error" class="form-error" role="alert">{{error}}<button @click="load">重试</button></div><div v-if="loading&&!book" class="room-loading">正在读取场次赔率…</div>
<div v-if="book" class="market-layout" :class="{regular:mode==='regular'}"><nav v-if="mode==='regular'" class="position-tabs" aria-label="选择投注位置"><button v-for="p in positions" :key="p" :class="{active:position===p}" @click="position=p">{{p}}<ChevronRight :size="14"/></button></nav><div class="market-groups"><section v-for="g in groups" :key="g.name" class="market-group"><div class="market-group-title"><h3>{{g.name}}</h3><small>{{mode==='two_sides'?'两面玩法':'定位大小单双 · 指定数字'}}</small></div><div v-for="(row,rowIndex) in g.rows" :key="rowIndex" class="market-options" :style="row[0]?.kind==='compare'?{gridTemplateColumns:`repeat(${row.length},minmax(0,1fr))`}:undefined" :class="{'market-numbers':row[0]?.kind==='number','market-compare':row[0]?.kind==='compare'}"><button v-for="m in row" :key="m.id" :disabled="busy||!quotes[m.id]?.enabled" :class="{selected:chosen?.market.id===m.id,numeric:m.kind==='number'}" :aria-label="`${g.name}${m.label}，赔率${oddsText(quotes[m.id]?.odds_milli??0)}`" :aria-pressed="chosen?.market.id===m.id" @click="select(m)"><strong>{{m.label}}</strong><span>{{quotes[m.id]?.enabled?`赔率 ${oddsText(quotes[m.id]!.odds_milli)}`:'已暂停'}}</span><Check v-if="chosen?.market.id===m.id" :size="13" class="market-selected"/></button></div></section></div></div>
<div class="market-selection"><div><small>当前选择</small><strong>{{chosen?`${chosen.market.group} · ${chosen.market.label}`:'请选择一个投注选项'}}</strong><p v-if="chosen">赔率 {{oddsText(chosen.odds)}}</p></div><button v-if="chosen" class="text-button" :disabled="busy" @click="chosen=null">清空</button></div><p v-if="changed||unavailable" class="form-error">{{unavailable?'该选项已暂停，请选择其他选项。':'赔率或单注金额已变更，请重新点击选项确认。'}}</p>
<StakeAmount v-model="amount" :min="playSetting?.unit_cents??200" :max="playSetting?.max_stake_cents??100000" :disabled="busy"/>
<div class="market-submit"><div class="market-totals"><span>投注金额 <b>{{chosen?money(cost):'0.00'}}</b></span><small>中奖返还（含本金） {{chosen?money(expected):'0.00'}}</small></div><button class="primary-button" :disabled="busy||blocked||!chosen||changed||unavailable||!!error||cost<(playSetting?.unit_cents??200)||cost>(playSetting?.max_stake_cents??100000)" @click="submit">{{busy?'提交中…':blocked?(blockLabel??'暂不可投注'):!user?'登录后投注':'确认投注'}}<ChevronRight :size="17"/></button></div></section></template>
