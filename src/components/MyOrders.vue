<script setup lang="ts">
import { formatIssue } from '../lib/issueDisplay'
import {displayGameName} from '../data'
import {ref,computed,watch,onUnmounted} from 'vue'
import {ChevronLeft,ChevronRight,Filter,RefreshCw} from 'lucide-vue-next'
import {api,user,account,money,type Order} from '../api/account'
interface OrderPage {items:Order[];total:number;page:number;page_size:number;total_stake_cents:number;total_payout_cents:number}
const emit=defineEmits<{back:[];login:[]}>()
const orders=ref<Order[]>([]),page=ref(1),total=ref(0),stake=ref(0),payout=ref(0),loading=ref(false),error=ref(''),filterOpen=ref(false),status=ref('')
const filters=[{value:'',label:'全部记录'},{value:'win',label:'已中奖'},{value:'lose',label:'未中奖'},{value:'pending',label:'未开奖'},{value:'push',label:'和局退本'},{value:'refunded',label:'已撤销'}]
const pages=computed(()=>Math.max(1,Math.ceil(total.value/10)))
const timeFormat=new Intl.DateTimeFormat('sv-SE',{timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false})
const timestamp=(value:string)=>value?timeFormat.format(new Date(value)):'—'
function result(o:Order){
 if(o.status==='pending')return 'pending'
 if(o.status==='refunded')return 'refunded'
 if(o.status!=='settled')return 'unknown'
 if(o.outcome==='push')return 'push'
 return o.outcome==='win'||!o.outcome&&o.payout_cents>0?'win':'lose'
}
const resultLabel=(o:Order)=>filters.find(f=>f.value===result(o))?.label??'处理中'
function selection(o:Order){return o.market_name||o.numbers.map(n=>o.session_id.startsWith('pk10')||o.session_id.startsWith('lhc')?String(n).padStart(2,'0'):String(n)).join('、')}
function gameName(o:Order){return o.session_name||displayGameName(o.game_name)}
let version=0
async function load(requestedPage=page.value){
 const id=user.value?.id
 if(!id)return
 const current=++version
 loading.value=true;error.value=''
 try{
  const data=await api<OrderPage>(`/orders?page=${requestedPage}${status.value?'&status='+status.value:''}`)
  if(current!==version||user.value?.id!==id)return
  orders.value=data.items;total.value=data.total;page.value=data.page;stake.value=data.total_stake_cents;payout.value=data.total_payout_cents
 }catch(e){if(current===version)error.value=(e as Error).message}
 finally{if(current===version)loading.value=false}
}
function filter(value:string){if(status.value===value)return;status.value=value;orders.value=[];total.value=0;stake.value=0;payout.value=0;void load(1)}
watch(()=>user.value?.id,()=>{
 version++;orders.value=[];page.value=1;total.value=0;stake.value=0;payout.value=0;status.value='';error.value='';loading.value=false
 void load(1)
},{immediate:true})
watch(()=>account.value,()=>{if(!loading.value)void load()})
onUnmounted(()=>{version++})
</script>
<template>
<section class="bet-records" :aria-busy="loading">
 <header class="bet-record-header"><button aria-label="返回个人中心" @click="emit('back')"><ChevronLeft :size="24"/></button><h1>彩票投注记录</h1><button aria-label="筛选投注记录" :aria-expanded="filterOpen" aria-controls="bet-record-filters" @click="filterOpen=!filterOpen"><Filter :size="21"/><i v-if="status"/></button></header>
 <nav v-if="filterOpen" id="bet-record-filters" class="bet-record-filters" aria-label="投注状态筛选"><button v-for="f in filters" :key="f.value" :aria-pressed="status===f.value" :class="{active:status===f.value}" :disabled="!user" @click="filter(f.value)">{{f.label}}</button><button :disabled="loading||!user" aria-label="刷新投注记录" @click="load()"><RefreshCw :size="13"/>刷新</button></nav>
 <template v-if="user">
  <div class="bet-record-totals" aria-label="投注记录汇总"><span>总注数 <b>{{total}}</b></span><span>总投注金额 <b>{{money(stake)}}元</b></span><span>总派彩金额 <b>{{money(payout)}}元</b></span></div>
  <div v-if="error" class="form-error" role="alert">{{error}} <button class="text-button" @click="load()">重试</button></div>
  <p v-if="loading" class="bet-record-empty" role="status">正在加载投注记录…</p>
  <div class="bet-record-list">
   <article v-for="o in orders" :key="o.id" class="bet-record">
    <h2>{{gameName(o)}}</h2><strong class="bet-record-result" :class="result(o)">{{resultLabel(o)}}</strong>
    <dl><div><dt>投注号码</dt><dd>{{selection(o)}}</dd></div><div><dt>投注期号</dt><dd :title="o.issue">{{formatIssue(o.issue)}}</dd></div><div><dt>投注金额</dt><dd>{{money(o.cost_cents)}}元</dd></div><div><dt>派彩金额</dt><dd>{{money(o.payout_cents)}}元</dd></div></dl>
    <time :datetime="o.created_at">{{timestamp(o.created_at)}}</time>
   </article>
  </div>
  <p v-if="!orders.length&&!loading&&!error" class="bet-record-empty">{{status?'暂无符合条件的投注记录':'暂无投注记录'}}</p>
  <p v-if="orders.length&&!loading&&!error&&pages===1" class="bet-record-end">已加载全部数据</p>
  <nav v-if="pages>1" class="bet-record-pagination" aria-label="投注记录分页"><span aria-live="polite">第 {{page}} / {{pages}} 页</span><div><button :disabled="page<=1||loading" aria-label="投注记录上一页" @click="load(page-1)"><ChevronLeft :size="16"/>上一页</button><button :disabled="page>=pages||loading" aria-label="投注记录下一页" @click="load(page+1)">下一页<ChevronRight :size="16"/></button></div></nav>
 </template>
 <div v-else class="bet-record-empty"><p>登录后查看投注记录</p><button class="primary-button" @click="emit('login')">登录 / 注册</button></div>
</section>
</template>
<style scoped>
.bet-records{min-height:100%;background:#fff;color:#30313b;font-family:Arial,'Microsoft YaHei','PingFang SC',sans-serif;padding-bottom:28px}
.bet-record-header{display:grid;grid-template-columns:42px minmax(0,1fr) 42px;align-items:center;min-height:52px;padding:0 10px;background:linear-gradient(110deg,#4e60ec,#8050e9);color:#fff}
.bet-record-header h1{text-align:center;font-size:19px;font-weight:500;margin:0}.bet-record-header button{position:relative;display:grid;place-items:center;min-height:42px}.bet-record-header i{position:absolute;top:7px;right:5px;width:6px;height:6px;border-radius:50%;background:#ffe29d}
.bet-record-totals{display:flex;align-items:center;flex-wrap:wrap;gap:7px 16px;padding:11px 18px 9px;font-size:13px;line-height:1.6;border-bottom:1px solid #f5f4f7}.bet-record-totals span{white-space:nowrap}.bet-record-totals b{font-weight:500;margin-left:5px;font-variant-numeric:tabular-nums}
.bet-record{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:9px 18px;padding:22px 24px 20px;border-bottom:1px solid #f2f1f5}.bet-record h2{font-size:16px;font-weight:500;margin:0;line-height:1.4;overflow-wrap:anywhere}.bet-record-result{font-size:14px;line-height:1.6;font-weight:500;text-align:right;white-space:nowrap;color:#41414c}.bet-record-result.win{color:#ae557b}.bet-record-result.pending{color:#7b6bad}.bet-record-result.push{color:#4c9c87}.bet-record-result.refunded{color:#92919a}
.bet-record dl{margin:0;min-width:0;grid-column:1;display:grid;gap:2px;font-size:14px;line-height:1.6}.bet-record dl>div{display:grid;grid-template-columns:67px minmax(0,1fr);gap:8px}.bet-record dt{white-space:nowrap}.bet-record dd{margin:0;overflow-wrap:anywhere;font-variant-numeric:tabular-nums}.bet-record time{align-self:center;grid-column:2;font-size:13px;text-align:right;white-space:nowrap;font-variant-numeric:tabular-nums}
.bet-record-empty,.bet-record-end{padding:25px 16px;text-align:center;font-size:14px;color:#6c6976}.bet-record-empty .primary-button{margin:20px auto 0}.bet-record-pagination{display:flex;align-items:center;justify-content:space-between;gap:10px;padding:22px 18px;font-size:12px;color:#716a7d}.bet-record-pagination>div{display:flex;gap:10px}.bet-record-pagination button{display:flex;align-items:center;gap:3px;padding:8px 4px}.bet-record-pagination button:disabled{opacity:.35;cursor:not-allowed}
.bet-record-filters{display:flex;flex-wrap:wrap;gap:8px;padding:13px 16px;border-bottom:1px solid #ece8f4}.bet-record-filters button{display:flex;align-items:center;gap:4px;font-size:12px;padding:7px 10px;border-radius:4px;background:#f4f1fa;color:#726782}.bet-record-filters button.active{background:#7655df;color:#fff}.bet-records>.form-error{margin:14px 16px}
@media(max-width:760px){.bet-records{min-height:100dvh}.bet-record-header{min-height:48px;padding:0 7px}.bet-record-header h1{font-size:18px}.bet-record-totals{padding:9px 12px;font-size:11px;gap:5px 10px}.bet-record-totals b{margin-left:4px}.bet-record{padding:17px 16px 19px;gap:9px 12px}.bet-record h2{font-size:15px}.bet-record dl{font-size:13px;gap:1px}.bet-record dl>div{grid-template-columns:57px minmax(0,1fr);gap:6px}.bet-record time{font-size:11px}.bet-record-result{font-size:14px}.bet-record-end{padding:22px 12px;font-size:13px}}
@media(max-width:350px){.bet-record{padding-left:12px;padding-right:12px;column-gap:8px}.bet-record dl{font-size:12px}.bet-record dl>div{grid-template-columns:53px minmax(0,1fr);gap:5px}.bet-record time{font-size:10px}.bet-record-totals{column-gap:8px;font-size:10px}}
</style>
