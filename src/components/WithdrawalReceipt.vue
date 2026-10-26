<script setup lang="ts">
import {ref,watch,onUnmounted} from 'vue'
import {ChevronLeft,RefreshCw} from 'lucide-vue-next'
import {account,user,money,datetime} from '../api/account'
import {loadWithdrawalDetail,type WithdrawalDetail} from '../api/wallet'
import {withdrawalRemark} from '../lib/withdrawalDisplay'
const props=defineProps<{requestId:string}>()
const emit=defineEmits<{back:[]}>()
const detail=ref<WithdrawalDetail|null>(null),loading=ref(false),error=ref('')
let generation=0
async function load(){
 const owner=user.value?.id,id=props.requestId,ticket=++generation
 if(!owner||!id)return
 loading.value=true;error.value=''
 try{const data=await loadWithdrawalDetail(id);if(ticket!==generation||user.value?.id!==owner||props.requestId!==id)return;detail.value=data}
 catch(e){if(ticket===generation)error.value=e instanceof Error?e.message:'账变详情读取失败'}
 finally{if(ticket===generation)loading.value=false}
}
watch([()=>props.requestId,()=>user.value?.id],()=>{generation++;detail.value=null;error.value='';loading.value=false;void load()},{immediate:true,flush:'sync'})
watch(()=>account.value,()=>{if(!loading.value)void load()})
onUnmounted(()=>generation++)
</script>
<template>
 <section class="withdrawal-receipt" :aria-busy="loading">
  <header><button type="button" aria-label="返回提款记录" @click="emit('back')"><ChevronLeft :size="26"/></button><h1>账变详情</h1><button type="button" aria-label="刷新账变详情" :disabled="loading||!user" @click="load"><RefreshCw :size="18"/></button></header>
  <p v-if="error" class="receipt-message" role="alert">{{error}} <button type="button" @click="load">重新读取</button></p>
  <p v-if="loading&&!detail" class="receipt-message" role="status">正在读取账变详情…</p>
  <dl v-if="detail">
   <div><dt>订单号:</dt><dd>{{detail.request.id}}</dd></div>
   <div><dt>会员名:</dt><dd>{{detail.request.username}}</dd></div>
   <div><dt>变动前金额:</dt><dd>{{detail.balance_before_cents===null?'未记录':money(detail.balance_before_cents)+'元'}}</dd></div>
   <div><dt>变动后金额:</dt><dd>{{detail.balance_after_cents===null?'未记录':money(detail.balance_after_cents)+'元'}}</dd></div>
   <div><dt>变动金额:</dt><dd>-{{money(detail.request.amount_cents)}}元</dd></div>
   <div><dt>手续费:</dt><dd>{{detail.request.withdrawal?money(detail.request.withdrawal.fee_cents)+'元':'未记录'}}</dd></div>
   <div><dt>类型:</dt><dd>在线取款</dd></div>
   <div><dt>时间:</dt><dd>{{datetime(detail.request.created_at)}}</dd></div>
   <div><dt>备注:</dt><dd>{{withdrawalRemark(detail.request.status,detail.request.review_note)}}</dd></div>
  </dl>
 </section>
</template>
<style scoped>
.withdrawal-receipt{background:#f1f1f1;min-height:100vh;min-height:100dvh;color:#252525;font-family:Arial,'PingFang SC','Microsoft YaHei',sans-serif;padding-bottom:36px}.withdrawal-receipt header{display:grid;grid-template-columns:42px 1fr 42px;align-items:center;min-height:52px;padding:0 12px;background:linear-gradient(110deg,#4f60f3,#8a52ed);color:#fff}.withdrawal-receipt header h1{text-align:center;margin:0;font-size:19px;font-weight:400}.withdrawal-receipt header button{display:grid;place-items:center;min-height:44px}.withdrawal-receipt dl{margin:0;background:#fff}.withdrawal-receipt dl>div{display:flex;gap:5px;min-width:0;align-items:baseline;padding:22px 28px;border-bottom:1px solid #f0f0f0;font-size:16px;line-height:1.6}.withdrawal-receipt dt{flex-shrink:0;white-space:nowrap}.withdrawal-receipt dd{margin:0;min-width:0;overflow-wrap:anywhere;font-variant-numeric:tabular-nums}.receipt-message{padding:20px;font-size:14px;background:#fff;line-height:1.8;margin:0}.receipt-message button{color:#7954d9;text-decoration:underline}button:disabled{opacity:.45}@media(max-width:760px){.withdrawal-receipt header{min-height:48px;padding:0 8px}.withdrawal-receipt dl>div{font-size:15px;padding:18px 20px;min-height:56px}.withdrawal-receipt header h1{font-size:18px}}button:focus-visible{outline:2px solid currentColor;outline-offset:-3px}
</style>
