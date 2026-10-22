<script setup lang="ts">
import {orderOddsText,orderPayoutText} from '../lib/marketOdds'
import {computed,onMounted,onUnmounted,ref,watch} from 'vue'
import {Search,RefreshCw,Pencil} from 'lucide-vue-next'
import {api,money,datetime,labels,type User} from '../api/account'
import {formatDrawTime} from '../lib/drawTime'
import type {Round} from '../api/games'
import AdminDrawEditor, {type NumberRule} from './AdminDrawEditor.vue'
interface Estimate {orders:number;stake_cents:number;payout_cents:number;user_profit_cents:number|null;unresolved_orders:number}
interface Bet {id:string;username:string;market_name:string;numbers:number[]|null;odds_milli:number;tier_odds_milli?:Record<string,number>;estimated_payout_tier?:string;estimated_payout_multiplier?:number;estimated_payout_odds_milli?:number;cost_cents:number;created_at:string;estimated_outcome:string;estimated_payout_cents:number|null;user_profit_cents:number|null}
interface SessionStatus {number_rule:NumberRule;session_id:string;name:string;issue:string;draw_at:string;round:Round|null;estimate:Estimate;orders:Bet[]}
interface Snapshot {all_users:boolean;betting_frozen_cents:number;as_of:string;user:User|null;summary:Estimate;sessions:SessionStatus[]}
const props=defineProps<{drawRevision?:number}>()
const emit=defineEmits<{'draw-changed':[]}>()
const username=ref(''),searched=ref(''),data=ref<Snapshot|null>(null),loading=ref(false),error=ref('')
const expanded=ref<string[]>([])
const editing=ref<(SessionStatus&{round:Round})|null>(null),clockOffset=ref(0),saved=ref('')
const sessions=computed(()=>data.value?.sessions.filter(s=>data.value?.all_users||s.estimate.orders>0)??[])
const signed=(value:number)=>(value>0?'+':'')+money(value)
const profitClass=(value:number|null)=>({positive:value!==null&&value>0,negative:value!==null&&value<0})
const outcomeLabels:Record<string,string>={win:'预计中奖',lose:'预计未中',push:'预计退本',manual:'待人工判定',unavailable:'待号码同步'}
let active=true,sequence=0,timer:ReturnType<typeof setInterval>|undefined
async function load(name:string,reset=false){
 const request=++sequence
 loading.value=true;error.value=''
 if(reset){data.value=null;expanded.value=[];saved.value=''}
 try{
  const result=await api<Snapshot>(`/admin/user-status?username=${encodeURIComponent(name)}`)
  if(active&&request===sequence){data.value=result;clockOffset.value=Date.parse(result.as_of)-Date.now()}
 }catch(e){if(active&&request===sequence)error.value=(e as Error).message}
 finally{if(active&&request===sequence)loading.value=false}
}
function search(){
 const name=username.value.trim()
 searched.value=name
 void load(name,true)
}
function editNumbers(session:SessionStatus){if(session.round&&session.number_rule){editing.value={...session,round:session.round};saved.value=''}}
function numbersSaved(){const name=editing.value?.name;editing.value=null;saved.value=`已更新 ${name} 本期号码。`;emit('draw-changed')}
watch(()=>props.drawRevision,()=>{void load(searched.value)})
function toggle(id:string){expanded.value=expanded.value.includes(id)?expanded.value.filter(value=>value!==id):[...expanded.value,id]}
onMounted(()=>{void load('');timer=setInterval(()=>{if(!loading.value&&document.visibilityState==='visible')void load(searched.value)},5000)})
onUnmounted(()=>{active=false;sequence++;clearInterval(timer)})
</script>
<template>
 <section class="user-status surface">
  <div class="status-heading"><div><h2>用户状态</h2><p>留空查看全部游戏，输入用户名查看个人 · 每5秒更新</p></div><button class="secondary-button" :disabled="loading" @click="load(searched)"><RefreshCw :size="14"/>刷新</button></div>
  <form class="status-search" @submit.prevent="search"><label for="status-username">用户名</label><div class="status-input"><Search :size="16"/><input id="status-username" v-model="username" placeholder="留空查看全部，或输入完整用户名" maxlength="32" autocomplete="off"/></div><button class="primary-button" type="submit" :disabled="loading">{{loading?'查询中…':'查看状态'}}</button></form>
  <p class="status-note">本期指各场次下一次开奖的期号，仅统计查询范围内尚未结算的有效押注。用户盈亏＝派彩（含本金）－下注金额；正数为用户盈利，负数为用户亏损，不扣平台佣金。</p>
  <p v-if="error" class="form-error" role="alert">{{error}}<template v-if="data">。以下为上次成功获取的数据。</template></p>
  <p v-if="saved" class="status-saved" role="status">{{saved}}</p>
  <p v-if="loading&&!data" class="loading-line" role="status">正在读取用户押注并试算…</p>
  <template v-if="data">
   <div v-if="data.user" class="status-user"><strong>{{data.user.username}}</strong><span class="badge" :class="data.user.status">{{labels[data.user.status]||data.user.status}}</span><span>可用余额 {{money(data.user.balance_cents)}}</span><span>投注冻结 {{money(data.betting_frozen_cents)}}</span><span>提现冻结 {{money(data.user.frozen_cents)}}</span><small>更新于 {{datetime(data.as_of)}} · 北京时间</small></div>
   <div v-else class="status-user"><strong>全部用户 · 全部游戏</strong><span>投注冻结合计 {{money(data.betting_frozen_cents)}}</span><small>更新于 {{datetime(data.as_of)}} · 北京时间</small></div>
   <div class="status-metrics">
    <article><span>本期下注金额</span><strong>{{money(data.summary.stake_cents)}}</strong><small>{{data.summary.orders}} 注 · {{data.sessions.filter(s=>s.estimate.orders>0).length}} 个有押注场次</small></article>
    <article><span>{{data.summary.unresolved_orders?'已试算部分派彩':'预计派彩（含本金）'}}</span><strong>{{money(data.summary.payout_cents)}}</strong><small>按当前采用号码及下单赔率计算</small></article>
    <article><span>{{data.all_users?'全部用户预计盈亏合计':'用户预计盈亏'}}</span><strong :class="profitClass(data.summary.user_profit_cents)">{{data.summary.user_profit_cents===null?'暂无法完整预估':signed(data.summary.user_profit_cents)}}</strong><small>{{data.summary.unresolved_orders?`${data.summary.unresolved_orders} 注待号码同步或人工判定`:'不含分销佣金收入'}}</small></article>
   </div>
   <p v-if="!sessions.length" class="status-empty">该用户在各场次本期暂无待结算押注。</p>
   <template v-else>
    <p class="status-note">预计结果会随采用号码、订单撤销及期号切换更新，最终以实际结算为准。展开场次可查看每笔下注结果。</p>
    <div class="data-table-wrap"><table class="data-table status-table"><thead><tr><th>场次 / 本期期号</th><th>当前采用号码</th><th>下注金额 / 注数</th><th>预计派彩</th><th>用户预计盈亏</th><th>明细</th></tr></thead><tbody>
     <template v-for="s in sessions" :key="s.session_id">
      <tr><td><strong>{{s.name}}</strong><small>{{s.issue}} 期</small><small>开奖 {{formatDrawTime(s.draw_at)}}</small></td><td class="status-numbers"><button v-if="s.round" class="status-edit-numbers" :disabled="!!error||!s.number_rule" :aria-label="`修改${s.name}第${s.issue}期当前采用号码`" title="点击修改本期号码" @click="editNumbers(s)"><span>{{s.round.display_numbers.join(',')}}</span><Pencil :size="13"/></button><span v-else>待预生成 / 同步</span></td><td>{{money(s.estimate.stake_cents)}}<small>{{s.estimate.orders}} 注</small></td><td>{{money(s.estimate.payout_cents)}}<small v-if="s.estimate.unresolved_orders">仅已试算部分</small></td><td :class="profitClass(s.estimate.user_profit_cents)"><strong>{{s.estimate.user_profit_cents===null?'暂无法完整预估':signed(s.estimate.user_profit_cents)}}</strong><small v-if="s.estimate.unresolved_orders">{{s.estimate.unresolved_orders}} 注未能试算</small></td><td><span v-if="!s.estimate.orders">暂无押注</span><button v-else class="text-button" :aria-expanded="expanded.includes(s.session_id)" :aria-controls="`bets-${s.session_id}`" @click="toggle(s.session_id)">{{expanded.includes(s.session_id)?'收起':'展开'}} {{s.estimate.orders}} 注</button></td></tr>
      <tr v-if="expanded.includes(s.session_id)" :id="`bets-${s.session_id}`" class="status-details"><td colspan="6"><table class="data-table bets-table" :aria-label="`${s.name}下注明细`"><thead><tr><th>{{data.all_users?'用户 / 订单 / 下单时间':'订单 / 下单时间'}}</th><th>下注内容 / 赔率</th><th>下注金额</th><th>预计结果</th><th>预计派彩</th><th>用户预计盈亏</th></tr></thead><tbody><tr v-for="bet in s.orders" :key="bet.id"><td><template v-if="data.all_users"><strong>{{bet.username||'—'}}</strong><br/></template>#{{bet.id}}<small>{{datetime(bet.created_at)}}</small></td><td>{{bet.market_name||bet.numbers?.join(',')||'单式下注'}}<small v-if="bet.odds_milli">赔率 {{orderOddsText(bet)}}</small></td><td>{{money(bet.cost_cents)}}</td><td>{{outcomeLabels[bet.estimated_outcome]||'暂无法预估'}}<small>{{orderPayoutText({payout_tier:bet.estimated_payout_tier,payout_multiplier:bet.estimated_payout_multiplier,payout_odds_milli:bet.estimated_payout_odds_milli})}}</small></td><td>{{bet.estimated_payout_cents===null?'—':money(bet.estimated_payout_cents)}}</td><td :class="profitClass(bet.user_profit_cents)">{{bet.user_profit_cents===null?'—':signed(bet.user_profit_cents)}}</td></tr></tbody></table></td></tr>
     </template>
    </tbody></table></div>
   </template>
  </template>
 </section>
 <AdminDrawEditor v-if="editing" :session="editing" :clock-offset="clockOffset" @close="editing=null" @saved="numbersSaved" @refresh="emit('draw-changed')"/>
</template>
<style scoped>
.status-edit-numbers{display:inline-flex;align-items:center;gap:7px;text-align:left;color:#8063b6;padding:5px 7px;margin:-5px -7px;border-radius:5px;max-width:100%;font-size:12px;font-variant-numeric:tabular-nums}.status-edit-numbers span{overflow-wrap:anywhere;min-width:0}.status-edit-numbers svg{flex-shrink:0;opacity:.7}.status-edit-numbers:hover:not(:disabled){background:#f1ebfa}.status-edit-numbers:focus-visible{outline:2px solid #9873c7;outline-offset:2px}.status-saved{color:#399b7b;font-size:12px;margin:10px 0}

.user-status{padding:20px}.status-heading{display:flex;align-items:center;justify-content:space-between;gap:12px}.status-heading h2{font-size:19px;margin:0}.status-heading p,.status-note{font-size:11px;line-height:1.7;color:#9588a1;margin:6px 0 12px}.status-search{display:flex;align-items:center;gap:10px;margin:14px 0}.status-search>label{font-size:12px;color:#776a88}.status-input{display:flex;align-items:center;gap:8px;border:1px solid #e6e0ed;border-radius:8px;padding:9px 12px;color:#a194ac;max-width:320px;flex:1;min-width:0}.status-input:focus-within{border-color:#9675c6}.status-input input{width:100%;min-width:0;border:0;outline:0;background:none;color:#5d536e;font-size:13px}.status-search button,.status-heading button{font-size:12px;padding:9px 14px}.status-user{display:flex;align-items:center;flex-wrap:wrap;gap:10px;padding:14px 0;border-top:1px solid #efeaf5;font-size:12px;color:#8c809d}.status-user>strong{font-size:16px;color:#5d536e}.status-user small{margin-left:auto;font-size:11px;color:#a092ac}.status-metrics{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;margin-bottom:16px}.status-metrics article{padding:16px;background:#faf8fd;border:1px solid #eee8f5;border-radius:10px;min-width:0}.status-metrics span{font-size:12px;color:#93839f}.status-metrics strong{display:block;font-size:23px;color:#706082;margin:9px 0;overflow-wrap:anywhere;font-variant-numeric:tabular-nums}.status-metrics small{font-size:11px;color:#a08eac}.status-table th,.status-table td{padding:10px 12px;font-size:12px}.status-table td small{font-size:10px;margin-top:3px}.status-table .status-numbers{white-space:normal;min-width:160px;max-width:250px;overflow-wrap:anywhere;font-variant-numeric:tabular-nums}.status-table .text-button{font-size:12px}.status-details>td{padding:10px;background:#faf8fd}.bets-table{background:white}.status-empty{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:10px;padding:35px 15px;text-align:center;color:#a090ad;font-size:12px}.negative{color:#c76878!important}.positive{color:#399b7b!important}.user-status button:disabled{opacity:.5;cursor:not-allowed}@media(max-width:640px){.user-status{padding:14px}.status-search{flex-wrap:wrap}.status-search>label{width:100%}.status-metrics{grid-template-columns:1fr;gap:8px}.status-metrics article{padding:12px;display:grid;grid-template-columns:1fr auto;gap:6px}.status-metrics strong{font-size:20px;margin:0}.status-metrics small{grid-column:1/-1}.status-user small{width:100%;margin-left:0}.status-heading p{max-width:230px}.status-heading button{flex-shrink:0}}
</style>
