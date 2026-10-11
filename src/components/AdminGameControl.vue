<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch,nextTick} from 'vue'
import {RefreshCw,SlidersHorizontal,Shuffle,X} from 'lucide-vue-next'
import {api,money,datetime} from '../api/account'
import type {Round} from '../api/games'
interface Profit {start:string;end:string;orders:number;stake_cents:number;payout_cents:number;gross_profit_cents:number;commission_cents:number;net_profit_cents:number}
interface Estimate {orders:number;stake_cents:number;payout_cents:number;commission_cents:number;net_profit_cents:number|null;unresolved_orders:number}
interface DrawPreview {current:Round;candidate:{id:string;round:Round;base_version:number}|null;cutoff_at:string;server_time:string;current_estimate:Estimate;candidate_estimate:Estimate|null}
interface SessionExposure {session_id:string;name:string;group:string;positions:number[];enabled:boolean;user_issue_limit_cents:number;pending_amount_cents:number;pending_orders:number;today:Profit;latest:Round|null;next:Round|null;next_draw_at:string;next_issue:string;next_estimate:Estimate}
interface Snapshot {as_of:string;today:Profit;yesterday:Profit;month:Profit;pending_amount_cents:number;pending_orders:number;undated_settled_orders:number;sessions:SessionExposure[]}
const emit=defineEmits<{settings:[];odds:[]}>()
const data=ref<Snapshot|null>(null),loading=ref(false),error=ref('')
const now=ref(Date.now()),clockOffset=ref(0)
let active=true,timer:ReturnType<typeof setInterval>|undefined,tick:ReturnType<typeof setInterval>|undefined,previewTimer:ReturnType<typeof setInterval>|undefined
const selected=ref<{session_id:string;name:string;issue:string;draw_at:string}|null>(null)
const preview=ref<DrawPreview|null>(null),previewBusy=ref(false),applying=ref(false),previewError=ref(''),savedMessage=ref('')
const dialog=ref<HTMLElement|null>(null)
let returnFocus:HTMLElement|null=null,previewSequence=0
const serverCutoff=(drawAt:string)=>Date.parse(drawAt)-5000
const cutoff=(drawAt:string)=>serverCutoff(drawAt)-1000
const cutoffMessage=(drawAt:string)=>now.value+clockOffset.value>=serverCutoff(drawAt)?'已超过开奖时间不可修改':'已到操作截止时间，不可修改'
const locked=computed(()=>!!selected.value&&now.value+clockOffset.value>=cutoff(selected.value.draw_at))
const randomDisabled=(s:SessionExposure)=>!s.next||!!error.value||now.value+clockOffset.value>=cutoff(s.next_draw_at)
async function openRandom(session:SessionExposure,event:MouseEvent){
 now.value=Date.now()
 if(randomDisabled(session))return
 returnFocus=event.currentTarget as HTMLElement
 selected.value={session_id:session.session_id,name:session.name,issue:session.next_issue,draw_at:session.next_draw_at}
 preview.value=null;previewError.value='';savedMessage.value=''
 await nextTick();dialog.value?.focus();void updatePreview()
}
function closeRandom(){
 if(applying.value)return
 previewSequence++;selected.value=null;preview.value=null;previewBusy.value=false;previewError.value=''
 returnFocus?.focus()
}
function trapDialog(event:KeyboardEvent){
 if(event.key!=='Tab')return
 const nodes=dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),[tabindex="0"]')
 if(!nodes?.length){event.preventDefault();return}
 const first=nodes[0]!,last=nodes[nodes.length-1]!,outside=!dialog.value?.contains(document.activeElement)
 if(event.shiftKey&&(outside||document.activeElement===first||document.activeElement===dialog.value)){event.preventDefault();last.focus()}
 else if(!event.shiftKey&&(outside||document.activeElement===last||document.activeElement===dialog.value)){event.preventDefault();first.focus()}
}
async function updatePreview(random=false){
 now.value=Date.now()
 const selection=selected.value
 if(!selection||locked.value||previewBusy.value||applying.value)return
 const sequence=++previewSequence
 previewBusy.value=true
 try{
  const value=await api<DrawPreview>(`/admin/game-control/${selection.session_id}/preview`,'POST',{issue:selection.issue,candidate_id:random?'':preview.value?.candidate?.id??'',random})
  if(!active||sequence!==previewSequence)return
  const received=Date.now();now.value=received;clockOffset.value=Date.parse(value.server_time)-received
  preview.value=value;previewError.value=''
 }catch(e){if(active&&sequence===previewSequence)previewError.value=(e as Error).message}
 finally{if(active&&sequence===previewSequence)previewBusy.value=false}
}
async function applyRandom(){
 now.value=Date.now()
 const selection=selected.value,candidate=preview.value?.candidate
 if(!selection||!candidate||locked.value||applying.value||previewBusy.value||previewError.value)return
 applying.value=true
 try{
  await api<Round>(`/admin/game-control/${selection.session_id}/apply`,'POST',{issue:selection.issue,candidate_id:candidate.id})
  if(!active)return
  savedMessage.value=`已采用 ${selection.name} 第 ${selection.issue} 期随机结果。`
  applying.value=false;closeRandom();void load()
 }catch(e){if(active)previewError.value=(e as Error).message}
 finally{if(active)applying.value=false}
}
const expired=computed(()=>data.value?.sessions.some(session=>remaining(session)<=0)??false)
function remaining(session:SessionExposure){return Math.max(0,Math.ceil((Date.parse(session.next_draw_at)-now.value-clockOffset.value)/1000))}
function countdown(session:SessionExposure){const seconds=remaining(session);return [Math.floor(seconds/3600),Math.floor(seconds%3600/60),seconds%60].map(n=>String(n).padStart(2,'0')).join(':')}
const numbers=(round:Round)=>round.display_numbers.join(',')
const drawTime=(value:string)=>new Date(value).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false})
watch(expired,value=>{if(value)void load()})
const periods=computed(()=>data.value?[{name:'今日盈亏',value:data.value.today},{name:'昨日盈亏',value:data.value.yesterday},{name:'本月盈亏',value:data.value.month}]:[])
const signed=(n:number)=>(n>0?'+':'')+money(n)
async function load(){
 if(loading.value)return
 loading.value=true
 const started=Date.now()
 try{
  const next=await api<Snapshot>('/admin/game-control')
  if(active){clockOffset.value=Date.parse(next.as_of)-(started+Date.now())/2;now.value=Date.now();data.value=next;error.value=''}
 }catch(e){if(active)error.value=(e as Error).message}
 finally{if(active)loading.value=false}
}
onMounted(()=>{void load();timer=setInterval(()=>void load(),5000);tick=setInterval(()=>now.value=Date.now(),250);previewTimer=setInterval(()=>void updatePreview(),2000)})
onUnmounted(()=>{active=false;previewSequence++;clearInterval(timer);clearInterval(tick);clearInterval(previewTimer)})
</script>
<template><div class="game-control"><div class="control-toolbar"><div><h2>游戏控制</h2><p>{{data?`统计更新于 ${datetime(data.as_of)}`:'正在读取统计'}} · 北京时间</p></div><button class="secondary-button" :disabled="loading" @click="load"><RefreshCw :size="15"/>刷新</button></div><div v-if="error" class="form-error" role="alert">统计更新失败：{{error}}。{{data?'当前显示上次成功获取的数据。':'请稍后重试。'}}</div><p v-if="savedMessage" class="saved-message" role="status">{{savedMessage}}</p><p v-if="loading&&!data" class="loading-line" role="status">正在汇总投注与结算…</p><template v-if="data"><div class="control-metrics"><article v-for="p in periods" :key="p.name" class="surface"><small>{{p.name}}（平台）</small><strong :class="{positive:p.value.net_profit_cents>0,negative:p.value.net_profit_cents<0}">{{signed(p.value.net_profit_cents)}}</strong><span>已结算 {{p.value.orders}} 注 · 扣除佣金后</span><dl><dt>投注金额</dt><dd>{{money(p.value.stake_cents)}}</dd><dt>派彩（含本金）</dt><dd>{{money(p.value.payout_cents)}}</dd><dt>佣金成本</dt><dd>{{money(p.value.commission_cents)}}</dd></dl></article><article class="surface pending-metric"><small>当前总押注中金额</small><strong>{{money(data.pending_amount_cents)}}</strong><span>{{data.pending_orders}} 注等待结算</span><p>包含待开奖、待自动结算及待人工结算订单；不计入已结算盈亏。</p></article></div>
<section class="surface control-explanation"><h3>统计口径</h3><p>盈亏＝已结算投注金额－含本金派彩－该批订单产生的佣金。正数为平台盈利，负数为平台亏损，按结算时间归入北京时间自然日或自然月；佣金无论是否领取均计入成本。</p><p>退款订单不计盈亏；和局退本净额为0。充值、提现、注册赠送和管理员调账不计入投注盈亏。测试余额参与的投注同样纳入统计，因此这里不代表真实现金利润。</p><p v-if="data.undated_settled_orders" class="negative">有 {{data.undated_settled_orders}} 笔已结算订单缺少结算时间，未计入日期盈亏，请在订单管理中核对。</p></section>
<section class="surface sessions-panel">
 <div class="control-toolbar"><div><h3>场次状态与号码来源</h3><p>预生成号码仅管理员可见 · 数据每5秒同步 · 倒计时每秒更新 · 北京时间</p></div><div class="control-actions"><button class="secondary-button" @click="emit('settings')"><SlidersHorizontal :size="15"/>接单与限额</button><button class="secondary-button" @click="emit('odds')">赔率设置</button></div></div>
 <p class="estimate-note">预计盈亏＝下一期当前已下注金额－预计派彩（含本金）－佣金；正数为平台盈利，随新增或撤销订单更新。人工结算订单无法完整预估。</p>
 <div class="data-table-wrap"><table class="data-table session-table">
  <thead><tr><th>游戏场次</th><th>上期开奖号码</th><th>下一期开奖号码</th><th>下期开奖 / 倒计时</th><th>预计下期盈亏</th><th>接单状态</th><th>单用户单期上限</th><th>今日盈亏</th><th>待结算金额 / 注数</th><th>号码源 / 位置</th></tr></thead>
  <tbody><tr v-for="s in data.sessions" :key="s.session_id">
   <td>{{s.name}}</td>
   <td class="session-draw"><template v-if="s.latest"><span class="plain-numbers">{{numbers(s.latest)}}</span><small>{{s.latest.issue}} 期<span v-if="s.latest.origin==='simulated_history'"> · 模拟历史</span></small></template><span v-else class="draw-muted">暂无开奖记录</span></td>
   <td class="session-draw"><div class="next-number-line"><span v-if="s.next" class="plain-numbers">{{numbers(s.next)}}</span><span v-else class="draw-muted">待预生成 / 同步</span><button class="random-button" :disabled="randomDisabled(s)" :aria-label="`${s.name}下一期随机试算`" :title="now+clockOffset>=cutoff(s.next_draw_at)?cutoffMessage(s.next_draw_at):'开奖上一分钟59秒起停止随机试算和确认采用'" @click="openRandom(s,$event)"><Shuffle :size="12"/>随机</button></div><small>{{s.next_issue}} 期</small><small v-if="now+clockOffset>=cutoff(s.next_draw_at)" class="draw-pending">{{cutoffMessage(s.next_draw_at)}}</small></td>
   <td class="session-next"><span>{{drawTime(s.next_draw_at)}}</span><small><strong class="draw-countdown" :class="{'draw-pending':remaining(s)<=10}">{{countdown(s)}}</strong><span v-if="remaining(s)===0"> · 等待更新</span></small></td>
   <td class="session-estimate"><template v-if="s.next_estimate.net_profit_cents!==null"><strong :class="{positive:s.next_estimate.net_profit_cents>0,negative:s.next_estimate.net_profit_cents<0}">{{signed(s.next_estimate.net_profit_cents)}}</strong></template><span v-else class="draw-muted">{{s.next?'待人工判定':'暂无法预估'}}</span><small>{{s.next_estimate.orders}} 注 · 押注 {{money(s.next_estimate.stake_cents)}}</small><small v-if="s.next&&s.next_estimate.unresolved_orders" class="draw-pending">{{s.next_estimate.unresolved_orders}} 注无法自动预估</small></td>
   <td><span class="badge" :class="s.enabled?'active':'disabled'">{{s.enabled?'已启用':'已暂停'}}</span></td>
   <td>{{money(s.user_issue_limit_cents)}}</td>
   <td :class="{positive:s.today.net_profit_cents>0,negative:s.today.net_profit_cents<0}">{{signed(s.today.net_profit_cents)}}</td>
   <td>{{money(s.pending_amount_cents)}}<small>{{s.pending_orders}} 注</small></td>
   <td>{{s.group}}<small>{{s.positions.join('/')}}</small></td>
  </tr></tbody>
 </table></div>
</section></template>
<Teleport to="body"><div v-if="selected" class="modal-overlay" @click.self="closeRandom" @keydown.esc.stop="closeRandom" @keydown="trapDialog">
 <section ref="dialog" class="modal random-dialog" role="dialog" aria-modal="true" aria-labelledby="random-title" tabindex="-1">
  <button class="modal-close icon-button" :disabled="applying" aria-label="关闭随机试算" @click="closeRandom"><X :size="20"/></button>
  <h2 id="random-title">下一期随机试算</h2>
  <p class="random-meta">{{selected.name}} · 第 {{selected.issue}} 期</p>
  <p class="random-meta">开奖 {{datetime(selected.draw_at)}} · 操作截止 {{datetime(new Date(cutoff(selected.draw_at)).toISOString())}}</p>
  <p class="random-note">随机仅试算，确认采用后替换此场次本期实际号码。按当前订单实时估算盈亏，正数为平台盈利。</p>
  <p v-if="locked" class="form-error" role="status">{{cutoffMessage(selected.draw_at)}}</p>
  <p v-else-if="previewError" class="form-error" role="alert">{{previewError}}</p>
  <p v-if="!preview&&previewBusy" class="loading-line">正在读取本期号码与押注…</p>
  <div v-if="preview" class="random-comparison">
   <article v-for="item in [{label:'当前采用号码',round:preview.current,estimate:preview.current_estimate},{label:'随机预览号码',round:preview.candidate?.round,estimate:preview.candidate_estimate}]" :key="item.label" class="random-result">
    <h3>{{item.label}}</h3>
    <p class="random-numbers">{{item.round?numbers(item.round):'点击随机生成新结果'}}</p>
    <template v-if="item.estimate"><dl><dt>当前押注</dt><dd>{{money(item.estimate.stake_cents)}}（{{item.estimate.orders}}注）</dd><dt>预计派彩</dt><dd>{{money(item.estimate.payout_cents)}}</dd><dt>预计盈亏</dt><dd :class="{positive:(item.estimate.net_profit_cents??0)>0,negative:(item.estimate.net_profit_cents??0)<0}"><strong>{{item.estimate.net_profit_cents===null?'无法完整预估':signed(item.estimate.net_profit_cents)}}</strong></dd></dl><p v-if="item.estimate.unresolved_orders" class="random-note">{{item.estimate.unresolved_orders}} 注需人工判定，预计派彩仅含已试算部分。</p></template>
   </article>
  </div>
  <p class="random-note random-reminder">为防止网络波动，请至少在开奖时间前 3 秒点击‘确认采用’</p>
  <div class="random-footer"><button class="secondary-button" :disabled="applying" @click="closeRandom">关闭</button><button class="secondary-button" :disabled="locked||previewBusy||applying||!preview" @click="updatePreview(true)"><Shuffle :size="14"/>{{previewBusy?'试算中…':'随机'}}</button><button class="primary-button" :disabled="locked||previewBusy||applying||!!previewError||!preview?.candidate" @click="applyRandom">{{applying?'正在采用…':'确认采用'}}</button></div>
 </section>
</div></Teleport>
</div></template>
<style scoped>
.random-dialog .random-note.random-reminder{color:#a57428}
.next-number-line{display:flex;align-items:center;gap:8px}.random-button{display:inline-flex;align-items:center;gap:3px;border:1px solid #e6e2ee;border-radius:4px;padding:2px 5px;font-size:10px;color:#8063b6;white-space:nowrap}.saved-message{font-size:12px;color:#399b7b;margin:0 0 14px}.random-dialog{width:min(620px,calc(100vw - 32px));max-width:620px;max-height:calc(100dvh - 40px);overflow:auto;padding:26px}.random-dialog h2{font-size:19px;padding-right:24px}.random-dialog .random-meta{font-size:11px;color:#8e849e;line-height:1.7;margin:8px 0}.random-dialog .random-note{font-size:11px;line-height:1.7;margin:12px 0;color:#9b8aaa}.random-comparison{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin:16px 0}.random-result{padding:15px;border:1px solid #ece8f2;border-radius:8px;min-width:0}.random-result h3{font-size:12px;color:#82758f}.random-dialog .random-numbers{font-size:14px;font-weight:600;font-variant-numeric:tabular-nums;color:#53586c;overflow-wrap:anywhere;line-height:1.7;margin:12px 0}.random-result dl{display:grid;grid-template-columns:auto 1fr;gap:9px;font-size:11px;color:#8e849e}.random-result dd{margin:0;text-align:right;font-variant-numeric:tabular-nums}.random-footer{display:flex;justify-content:flex-end;gap:8px;margin-top:18px}.random-footer button{font-size:12px;padding:9px 12px}@media(max-width:480px){.random-comparison{grid-template-columns:1fr}.random-dialog{padding:22px 18px}}

.sessions-panel{padding:18px}.sessions-panel .control-toolbar{margin-bottom:10px}.estimate-note{font-size:10px;line-height:1.7;color:#9b8aaa;margin-bottom:12px}.session-table{font-size:11px}.session-table th,.session-table td{padding:9px 10px;line-height:1.45}.session-table td small{font-size:10px;margin-top:3px}.plain-numbers{font-size:12px;font-weight:500;font-variant-numeric:tabular-nums;color:#53586c}.session-next{min-width:125px}.draw-countdown{font-size:12px;font-weight:600;font-variant-numeric:tabular-nums;color:#62677e}.session-estimate strong{font-size:12px;font-variant-numeric:tabular-nums}.session-table .badge{padding:3px 6px;font-size:10px}.draw-muted{color:#a092b1}.draw-pending{color:#b58637!important}

.control-toolbar{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:20px}.control-toolbar h2{font-size:20px}.control-toolbar p{font-size:11px;color:#a092b1;margin-top:8px;line-height:1.8}.control-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:18px;margin-bottom:20px}.control-metrics article{padding:24px;min-width:0}.control-metrics small{font-size:12px;color:#9989a8}.control-metrics strong{display:block;font-size:26px;color:#8063b6;margin:16px 0 9px;overflow-wrap:anywhere;font-variant-numeric:tabular-nums}.control-metrics span,.pending-metric p{font-size:11px;color:#a295ae;line-height:1.9}.pending-metric p{margin-top:24px}.control-metrics dl{display:grid;grid-template-columns:1fr auto;gap:12px 6px;border-top:1px solid #efedf5;margin:20px 0 0;padding-top:18px;font-size:11px;color:#9887a8}.control-metrics dd{margin:0;font-variant-numeric:tabular-nums}.control-explanation{margin-bottom:20px}.control-explanation p{font-size:12px;color:#9b8aaa;line-height:1.9;margin-top:10px}.control-actions{display:flex;gap:10px;flex-shrink:0}.positive{color:#399b7b!important}.negative{color:#c76878!important}@media(max-width:1200px){.control-metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:640px){.control-metrics{gap:10px}.control-metrics article{padding:16px 12px}.control-metrics strong{font-size:22px}.control-metrics dl{grid-template-columns:1fr;gap:5px}.control-metrics dd{margin-bottom:6px}.control-toolbar{flex-wrap:wrap}.control-actions{width:100%;flex-wrap:wrap}.control-actions button{font-size:11px}.control-explanation{padding:18px}}
</style>
