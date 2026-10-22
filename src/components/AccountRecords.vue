<script setup lang="ts">
import PaymentRequestDetails from './PaymentRequestDetails.vue'
import {ref,computed,watch,onUnmounted} from 'vue'
import {ChevronLeft,ChevronRight} from 'lucide-vue-next'
import {api,user,account,money,datetime,labels,type Ledger,type WalletRequest} from '../api/account'
const props=defineProps<{kind:'ledger'|'transactions'|'daily'}>()
interface DailyResult {date:string;orders:number;stake_cents:number;payout_cents:number;net_cents:number}
interface RecordPage<T>{items:T[];total:number;page:number;page_size:number}
const ledger=ref<Ledger[]>([]),transactions=ref<WalletRequest[]>([]),days=ref<DailyResult[]>([])
const page=ref(1),total=ref(0),loading=ref(false),error=ref('')
const pages=computed(()=>Math.max(1,Math.ceil(total.value/10)))
const count=computed(()=>props.kind==='ledger'?ledger.value.length:props.kind==='transactions'?transactions.value.length:days.value.length)
const title=computed(()=>({ledger:'账变记录',transactions:'交易记录',daily:'每日输赢'}[props.kind]))
const walletLabel=(value:string)=>({deposit:'存款',withdraw:'提款',withdraw_hold:'提款冻结',withdraw_approved:'提款完成',withdraw_rejected:'提款解冻',deposit_approved:'存款入账'}[value]??labels[value]??value)
let version=0
async function load(requested=page.value){
 const id=user.value?.id;if(!id)return
 const current=++version;loading.value=true;error.value=''
 try{
  const endpoint={ledger:'ledger',transactions:'transactions',daily:'daily-results'}[props.kind]
  const data=await api<RecordPage<Ledger|WalletRequest|DailyResult>>(`/account/${endpoint}?page=${requested}`)
  if(current!==version||user.value?.id!==id)return
  if(props.kind==='ledger')ledger.value=data.items as Ledger[]
  else if(props.kind==='transactions')transactions.value=data.items as WalletRequest[]
  else days.value=data.items as DailyResult[]
  total.value=data.total;page.value=data.page
 }catch(e){if(current===version)error.value=(e as Error).message}
 finally{if(current===version)loading.value=false}
}
watch(()=>user.value?.id,()=>{version++;ledger.value=[];transactions.value=[];days.value=[];page.value=1;total.value=0;error.value='';void load(1)},{immediate:true})
watch(()=>account.value,()=>{if(!loading.value)void load()})
onUnmounted(()=>version++)
</script>
<template>
 <section class="surface account-records" :aria-busy="loading">
  <h3>{{title}}<small>金额单位：元</small></h3>
  <p v-if="kind==='daily'" class="daily-rule">按北京时间投注日期汇总，净输赢＝已结算派彩－已结算投注；待结算及已撤销订单不计入。</p>
  <div v-if="error" class="form-error" role="alert">{{error}} <button type="button" class="text-button" @click="load()">重试</button></div>
  <p v-if="loading" class="loading-line" role="status">正在读取记录…</p>
  <div v-if="kind==='ledger'" class="record-cards ledger-records"><article v-for="entry in ledger" :key="entry.id"><div class="record-top"><strong>{{walletLabel(entry.kind)}}</strong><b :class="{positive:entry.amount_cents>0}">{{entry.amount_cents>0?'+':''}}{{money(entry.amount_cents)}}</b></div><time>{{datetime(entry.created_at)}}</time><p v-if="entry.note">{{entry.note}}</p><dl><div><dt>余额</dt><dd>{{money(entry.balance_cents)}}</dd></div><div><dt>提款冻结</dt><dd>{{money(entry.frozen_cents)}}</dd></div></dl></article></div>
  <div v-else-if="kind==='transactions'" class="record-cards transaction-records"><article v-for="request in transactions" :key="request.id"><div class="record-top"><strong>{{walletLabel(request.kind)}} · {{money(request.amount_cents)}}</strong><span class="badge" :class="request.status">{{labels[request.status]||request.status}}</span></div><time>{{datetime(request.created_at)}}</time><PaymentRequestDetails v-if="request.withdrawal" :request="request"/><p v-if="request.payment_method==='usdt'&&!request.withdrawal">USDT {{request.usdt_amount}} · {{request.payment_network}}<br/>交易 ID：{{request.transaction_id}}</p><p v-if="request.payment_method!=='usdt'&&request.transaction_id">交易 ID：{{request.transaction_id}}</p><p v-if="request.review_note||request.note">{{request.review_note||request.note}}</p></article></div>
  <table v-else-if="days.length" class="daily-results"><thead><tr><th>日期</th><th>已结算投注</th><th>派彩</th><th>净输赢</th></tr></thead><tbody><tr v-for="day in days" :key="day.date"><th>{{day.date}}<small>{{day.orders}} 笔</small></th><td>{{money(day.stake_cents)}}</td><td>{{money(day.payout_cents)}}</td><td :class="{positive:day.net_cents>0,negative:day.net_cents<0}">{{day.net_cents>0?'+':''}}{{money(day.net_cents)}}</td></tr></tbody></table>
  <p v-if="!count&&!loading&&!error" class="empty-copy">{{kind==='daily'?'暂无已结算的输赢记录':'暂无记录'}}</p>
  <nav class="record-pagination" :aria-label="`${title}分页`"><span aria-live="polite">共 {{total}} {{kind==='daily'?'天':'条'}} · 第 {{page}} / {{pages}} 页</span><div><button :disabled="page<=1||loading" aria-label="记录上一页" @click="load(page-1)"><ChevronLeft :size="16"/>上一页</button><button :disabled="page>=pages||loading" aria-label="记录下一页" @click="load(page+1)">下一页<ChevronRight :size="16"/></button></div></nav>
 </section>
</template>
<style scoped>
.account-records{min-width:0}.account-records h3{display:flex;align-items:center;justify-content:space-between;gap:10px}.account-records h3 small{font-size:11px;font-weight:400;color:#9c95a6}
.daily-rule{margin:15px 0;font-size:12px;color:#96909e;line-height:1.8}
.record-cards article{padding:20px 0;border-bottom:1px solid #ece9f1}.record-top{display:flex;align-items:center;justify-content:space-between;gap:12px;font-size:15px}.record-top strong,.record-top b{font-weight:500}.record-top b{font-variant-numeric:tabular-nums}.record-cards time{display:block;font-size:12px;color:#9990a3;margin:9px 0}.record-cards p{font-size:13px;color:#897e95;line-height:1.8;overflow-wrap:anywhere;white-space:pre-wrap}.record-cards dl{display:flex;gap:22px;margin:12px 0 0;font-size:12px}.record-cards dl>div{display:flex;gap:8px}.record-cards dt{color:#9c93a7}.record-cards dd{margin:0;color:#756983}
.daily-results{width:100%;table-layout:fixed;border-collapse:collapse;font-variant-numeric:tabular-nums}.daily-results th,.daily-results td{padding:18px 8px;text-align:center;font-size:14px;border-bottom:1px solid #eeeaf3;overflow-wrap:anywhere;font-weight:400}.daily-results thead th{background:#faf8fd;font-size:12px;color:#877b96}.daily-results th:first-child{width:32%}.daily-results tbody th{font-size:13px}.daily-results small{display:block;margin-top:7px;font-size:10px;color:#a196ae}.negative{color:#b75b73}
.record-pagination{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;margin-top:20px;font-size:12px;color:#93899f}.record-pagination>div{display:flex;gap:10px}.record-pagination button{display:flex;align-items:center;gap:3px;padding:8px;border:1px solid #e7e0ef;border-radius:5px;color:#7c639f}
@media(max-width:360px){.daily-results th,.daily-results td{padding:16px 4px;font-size:12px}.daily-results tbody th{font-size:11px}.daily-results thead th{font-size:10px}.record-top{font-size:13px}.record-cards dl{gap:12px;font-size:11px}}
</style>
