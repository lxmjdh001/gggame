<script setup lang="ts">
import {onMounted,onUnmounted,ref,watch} from 'vue'
import {BellRing,ChevronRight,Wallet,X} from 'lucide-vue-next'
import {api,APIError,money,datetime,type WalletRequest} from '../api/account'
interface Pending {total:number;deposit_count:number;withdraw_count:number;deposit_amount_cents:number;withdraw_amount_cents:number;latest_id:string;items:WalletRequest[]}
const props=defineProps<{revision:number}>()
const emit=defineEmits<{review:[request:WalletRequest];'view-all':[];updated:[]}>()
const data=ref<Pending|null>(null),expanded=ref(false),error=ref(''),busy=ref(false)
let active=true,sequence=0,highestSeen='',signature='',timer:ReturnType<typeof setInterval>|undefined
async function refresh(force=false){
 if(!active||busy.value&&!force)return
 const request=++sequence
 busy.value=true
 try{
  const next=await api<Pending>('/admin/requests/pending')
  if(!active||request!==sequence)return
  if(next.latest_id>highestSeen){highestSeen=next.latest_id;expanded.value=true}
  if(!next.total)expanded.value=false
  const nextSignature=JSON.stringify(next)
  const changed=signature!==''&&nextSignature!==signature
  signature=nextSignature;data.value=next;error.value=''
  if(changed)emit('updated')
 }catch(e){
  if(!active||request!==sequence)return
  if(e instanceof APIError&&(e.status===401||e.status===403)){data.value=null;expanded.value=false;error.value=''}
  else error.value='待处理提醒更新失败，正在重试'
 }finally{if(active&&request===sequence)busy.value=false}
}
function review(request:WalletRequest){expanded.value=false;emit('review',request)}
function viewAll(){expanded.value=false;emit('view-all')}
function visibilityChanged(){if(document.visibilityState==='visible')void refresh(true)}
watch(()=>props.revision,()=>void refresh(true))
onMounted(()=>{void refresh();timer=setInterval(()=>{if(document.visibilityState==='visible')void refresh()},5000);document.addEventListener('visibilitychange',visibilityChanged)})
onUnmounted(()=>{active=false;sequence++;clearInterval(timer);document.removeEventListener('visibilitychange',visibilityChanged)})
</script>
<template>
 <Teleport to="body">
  <aside v-if="data?.total||error" class="funds-notice" aria-label="资金申请待处理提醒">
   <button v-if="!expanded" class="funds-trigger" :aria-expanded="false" aria-controls="funds-pending-panel" @click="expanded=true"><BellRing :size="17"/><span>{{error?'资金提醒更新异常':`资金待处理 ${data?.total??0} 笔`}}</span><ChevronRight :size="15"/></button>
   <section v-else id="funds-pending-panel" class="funds-card">
    <header><span class="funds-icon"><Wallet :size="19"/></span><div><h2>资金申请待处理</h2><p role="status">{{data?.total??0}} 笔待审核 · 充值 {{data?.deposit_count??0}} / 提现 {{data?.withdraw_count??0}}</p></div><button class="funds-close" aria-label="收起资金待处理提醒" @click="expanded=false"><X :size="17"/></button></header>
    <p v-if="error" class="funds-error" role="status">{{error}}</p>
    <div v-if="data?.total" class="funds-totals"><span>充值 <b>{{money(data.deposit_amount_cents)}}</b></span><span>提现 <b>{{money(data.withdraw_amount_cents)}}</b></span></div>
    <ul v-if="data?.items.length" class="funds-list">
     <li v-for="request in data.items" :key="request.id"><div><strong>{{request.username}}</strong><span :class="{'funds-withdraw':request.kind==='withdraw'}">{{request.kind==='withdraw'?'提现':'充值'}} {{money(request.amount_cents)}}</span><small>#{{request.id.replace(/^0+/,'')||'0'}} · {{datetime(request.created_at)}}</small></div><button :disabled="!!error" :aria-label="`审核${request.username}的${request.kind==='withdraw'?'提现':'充值'}申请${request.id.replace(/^0+/,'')}`" @click="review(request)">审核<ChevronRight :size="13"/></button></li>
    </ul>
    <footer><span>{{data&&data.total>5?'显示最近5笔':'每5秒更新'}}</span><button @click="viewAll">前往资金审核<ChevronRight :size="14"/></button></footer>
   </section>
  </aside>
 </Teleport>
</template>
<style scoped>
.funds-notice{position:fixed;right:24px;top:88px;z-index:80;width:340px;max-width:calc(100vw - 32px);color:#5e5771}
.funds-trigger{display:flex;align-items:center;gap:8px;margin-left:auto;border:1px solid #ded5f8;background:#fff;border-radius:12px;padding:12px 14px;box-shadow:0 8px 30px #4931791c;color:#7556cf;font-size:12px;font-weight:600}
.funds-trigger>svg:first-child{color:#9c71dc}.funds-card{background:#fff;border:1px solid #e8e0f5;border-radius:16px;box-shadow:0 12px 40px #49317926;overflow:hidden;max-height:calc(100dvh - 108px);display:flex;flex-direction:column;text-align:left}
.funds-card header{display:flex;align-items:center;gap:10px;padding:15px 16px 12px;flex-shrink:0;background:linear-gradient(120deg,#faf7ff,#fff)}
.funds-icon{display:grid;place-items:center;width:36px;height:36px;border-radius:10px;background:#eee6ff;color:#8463d4;flex-shrink:0}.funds-card h2{font-size:14px;line-height:1.5;margin:0;color:#5b4b78}.funds-card header p{font-size:11px;margin:3px 0 0;color:#9a8bad}.funds-close{display:grid;place-items:center;padding:5px;margin-left:auto;align-self:flex-start;color:#a096b0;border-radius:5px}.funds-close:hover{background:#f0ebf7}
.funds-totals{display:flex;gap:16px;font-size:11px;padding:10px 16px;background:#fbf9fe;border-block:1px solid #f0eaf8;color:#9a8bad}.funds-totals b{font-weight:600;color:#7d6a97;font-variant-numeric:tabular-nums}.funds-list{list-style:none;margin:0;padding:0 16px;overflow:auto;min-height:0;overscroll-behavior:contain}.funds-list li{display:flex;align-items:center;gap:12px;padding:12px 0;border-bottom:1px solid #f1ecf7}.funds-list li:last-child{border-bottom:0}.funds-list li>div{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:5px 10px;flex:1;min-width:0}.funds-list strong{font-size:12px;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.funds-list span{font-size:12px;color:#399b7b;font-variant-numeric:tabular-nums}.funds-list .funds-withdraw{color:#bc8560}.funds-list small{grid-column:1/-1;font-size:10px;color:#a399ae}.funds-list button{display:flex;align-items:center;gap:1px;flex-shrink:0;padding:5px 0 5px 5px;font-size:11px;color:#8a68c9}.funds-list button:disabled{opacity:.5;cursor:not-allowed}.funds-card footer{display:flex;align-items:center;justify-content:space-between;gap:10px;padding:11px 16px;border-top:1px solid #eee7f6;flex-shrink:0}.funds-card footer span{font-size:10px;color:#a99bb7}.funds-card footer button{display:flex;align-items:center;gap:4px;font-size:12px;color:#8562c7}.funds-error{font-size:11px;color:#b17a36;padding:8px 16px;margin:0;background:#fffaf0}
@media(max-width:640px){.funds-notice{top:12px;right:12px;max-width:calc(100vw - 24px)}.funds-card{max-height:calc(100dvh - 100px)}.funds-trigger{padding:10px 12px}}
</style>
