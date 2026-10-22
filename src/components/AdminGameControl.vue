<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch,nextTick} from 'vue'
import {RefreshCw,SlidersHorizontal,Shuffle,X,RotateCcw} from 'lucide-vue-next'
import {api,money,datetime,key} from '../api/account'
import {formatDrawTime} from '../lib/drawTime'
import type {Round} from '../api/games'
import AdminUserStatus from './AdminUserStatus.vue'
interface Profit {start:string;end:string;orders:number;stake_cents:number;payout_cents:number;gross_profit_cents:number;commission_cents:number;net_profit_cents:number}
interface Estimate {orders:number;stake_cents:number;payout_cents:number;commission_cents:number;net_profit_cents:number|null;unresolved_orders:number}
interface Candidate {id:string;round:Round;base_version:number}
interface DrawPreview {current:Round;candidates:{candidate:Candidate;estimate:Estimate}[];cutoff_at:string;server_time:string;current_estimate:Estimate}
interface SessionExposure {session_id:string;name:string;group:string;positions:number[];enabled:boolean;user_issue_limit_cents:number;pending_amount_cents:number;pending_orders:number;today:Profit;latest:Round|null;next:Round|null;next_draw_at:string;next_issue:string;next_estimate:Estimate}
interface Snapshot {reset_at?:string;as_of:string;today:Profit;yesterday:Profit;month:Profit;pending_amount_cents:number;pending_orders:number;undated_settled_orders:number;sessions:SessionExposure[]}
const emit=defineEmits<{settings:[];odds:[]}>()
const data=ref<Snapshot|null>(null),loading=ref(false),error=ref('')
const resetDialog=ref<HTMLDialogElement|null>(null),resetBusy=ref(false),resetError=ref(''),resetKey=ref('')
let resetReturnFocus:HTMLElement|null=null
function openReset(event:MouseEvent){
 resetReturnFocus=event.currentTarget as HTMLElement;resetError.value='';resetKey.value=key();resetDialog.value?.showModal()
}
function closeReset(){if(!resetBusy.value)resetDialog.value?.close()}
function resetBackdrop(event:MouseEvent){
 if(event.target!==resetDialog.value||!resetDialog.value)return
 const r=resetDialog.value.getBoundingClientRect()
 if(event.clientX<r.left||event.clientX>r.right||event.clientY<r.top||event.clientY>r.bottom)closeReset()
}
async function resetRecords(){
 if(resetBusy.value)return
 resetBusy.value=true;resetError.value='';loadSequence++;loading.value=false
 try{
  await api('/admin/game-control/reset','POST',{key:resetKey.value})
  if(!active)return
  savedMessage.value='顶部4项统计已重置，之后仅统计新提交的订单。'
  await load(true)
  resetDialog.value?.close()
 }catch(e){if(active)resetError.value=(e as Error).message}
 finally{resetBusy.value=false}
}
const now=ref(Date.now()),clockOffset=ref(0),drawRevision=ref(0)
let loadSequence=0
let active=true,timer:ReturnType<typeof setInterval>|undefined,tick:ReturnType<typeof setInterval>|undefined,previewTimer:ReturnType<typeof setInterval>|undefined
const selected=ref<{session_id:string;name:string;issue:string;draw_at:string}|null>(null)
const preview=ref<DrawPreview|null>(null),previewBusy=ref(false),applying=ref(false),previewError=ref(''),savedMessage=ref('')
const candidates=ref<{candidate:Candidate;estimate:Estimate|null}[]>([]),selectedCandidateId=ref(''),randomCount=ref(0)
const selectedCandidate=computed(()=>candidates.value.find(item=>item.candidate.id===selectedCandidateId.value)?.candidate)
const comparisonItems=computed(()=>[
 {id:'current',label:'当前采用',round:preview.value?.current,estimate:preview.value?.current_estimate},
 ...candidates.value.map((item,index)=>({id:item.candidate.id,label:`随机结果${candidates.value.length>1?` ${index+1}`:''}`,round:item.candidate.round,estimate:item.estimate})),
 ...(candidates.value.length?[]:[{id:'empty',label:'随机结果',round:null,estimate:null}])
])
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
 preview.value=null;candidates.value=[];selectedCandidateId.value='';randomCount.value=0;previewError.value='';savedMessage.value=''
 await nextTick();dialog.value?.focus();void updatePreview()
}
function closeRandom(){
 if(applying.value)return
 previewSequence++;selected.value=null;preview.value=null;candidates.value=[];selectedCandidateId.value='';randomCount.value=0;previewBusy.value=false;previewError.value=''
 returnFocus?.focus()
}
function trapDialog(event:KeyboardEvent){
 if(event.key!=='Tab')return
 const nodes=dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),[tabindex="0"]')
 if(!nodes?.length){event.preventDefault();return}
 const first=nodes[0]!,last=nodes[nodes.length-1]!,outside=!dialog.value?.contains(document.activeElement)
 if(event.shiftKey&&(outside||document.activeElement===first||document.activeElement===dialog.value)){event.preventDefault();last.focus()}
 else if(!event.shiftKey&&(outside||document.activeElement===last||document.activeElement===dialog.value)){event.preventDefault();first.focus()}
}
async function updatePreview(random=false,count=1){
 now.value=Date.now()
 const selection=selected.value
 if(!selection||locked.value||previewBusy.value||applying.value)return
 const sequence=++previewSequence
 previewBusy.value=true;randomCount.value=random?count:0
 try{
  const value=await api<DrawPreview>(`/admin/game-control/${selection.session_id}/preview-batch`,'POST',{
   issue:selection.issue,count:random?count:0,candidate_ids:random?[]:candidates.value.map(item=>item.candidate.id)
  })
  if(!active||sequence!==previewSequence)return
  const received=Date.now();now.value=received;clockOffset.value=Date.parse(value.server_time)-received
  preview.value=value;candidates.value=value.candidates
  if(random)selectedCandidateId.value=count===1?candidates.value[0]?.candidate.id??'':''
  previewError.value=''
 }catch(e){if(active&&sequence===previewSequence)previewError.value=(e as Error).message}
 finally{if(active&&sequence===previewSequence){previewBusy.value=false;randomCount.value=0}}
}
async function applyRandom(){
 now.value=Date.now()
 const selection=selected.value,candidate=selectedCandidate.value
 if(!selection||!candidate||locked.value||applying.value||previewBusy.value||previewError.value)return
 applying.value=true
 try{
  await api<Round>(`/admin/game-control/${selection.session_id}/apply`,'POST',{issue:selection.issue,candidate_id:candidate.id})
  if(!active)return
  savedMessage.value=`已采用 ${selection.name} 第 ${selection.issue} 期随机结果。`
  applying.value=false;closeRandom();drawChanged()
 }catch(e){if(active){previewError.value=`${(e as Error).message}。请核对当前号码后再操作。`;drawChanged()}}
 finally{if(active)applying.value=false}
}
const expired=computed(()=>data.value?.sessions.some(session=>remaining(session)<=0)??false)
function remaining(session:SessionExposure){return Math.max(0,Math.ceil((Date.parse(session.next_draw_at)-now.value-clockOffset.value)/1000))}
function countdown(session:SessionExposure){const seconds=remaining(session);return [Math.floor(seconds/3600),Math.floor(seconds%3600/60),seconds%60].map(n=>String(n).padStart(2,'0')).join(':')}
const numbers=(round:Round)=>round.display_numbers.join(',')
const drawTime=(value:string)=>formatDrawTime(value,'short')
watch(expired,value=>{if(value)void load()})
const periods=computed(()=>data.value?[{name:'今日盈亏',value:data.value.today},{name:'昨日盈亏',value:data.value.yesterday},{name:'本月盈亏',value:data.value.month}]:[])
const signed=(n:number)=>(n>0?'+':'')+money(n)
function drawChanged(){drawRevision.value++;void load(true)}
async function load(force=false){
 if((loading.value||resetBusy.value)&&!force)return
 const request=++loadSequence
 loading.value=true
 const started=Date.now()
 try{
  const next=await api<Snapshot>('/admin/game-control')
  if(active&&request===loadSequence){clockOffset.value=Date.parse(next.as_of)-(started+Date.now())/2;now.value=Date.now();data.value=next;error.value=''}
 }catch(e){if(active&&request===loadSequence)error.value=(e as Error).message}
 finally{if(active&&request===loadSequence)loading.value=false}
}
onMounted(()=>{void load();timer=setInterval(()=>void load(),5000);tick=setInterval(()=>now.value=Date.now(),250);previewTimer=setInterval(()=>void updatePreview(),2000)})
onUnmounted(()=>{active=false;previewSequence++;clearInterval(timer);clearInterval(tick);clearInterval(previewTimer)})
</script>
<template><div class="game-control"><div class="control-toolbar"><div><h2>游戏控制</h2><p>{{data?`统计更新于 ${datetime(data.as_of)}`:'正在读取统计'}} · 北京时间</p></div><div class="control-actions"><button class="secondary-button" :disabled="!data||loading||resetBusy" @click="openReset"><RotateCcw :size="15"/>重置记录</button><button class="secondary-button" :disabled="loading||resetBusy" @click="load()"><RefreshCw :size="15"/>刷新</button></div></div><p v-if="data?.reset_at" class="control-reset-note">上次重置：{{datetime(data.reset_at)}} · 顶部4张卡片仅统计重置后新增订单，完整数据仍可在订单管理及下方场次表查看。</p><div v-if="error" class="form-error" role="alert">统计更新失败：{{error}}。{{data?'当前显示上次成功获取的数据。':'请稍后重试。'}}</div><p v-if="savedMessage" class="saved-message" role="status">{{savedMessage}}</p><p v-if="loading&&!data" class="loading-line" role="status">正在汇总投注与结算…</p><template v-if="data"><div class="control-metrics"><article v-for="p in periods" :key="p.name" class="surface"><small>{{p.name}}（平台）</small><strong :class="{positive:p.value.net_profit_cents>0,negative:p.value.net_profit_cents<0}">{{signed(p.value.net_profit_cents)}}</strong><span>已结算 {{p.value.orders}} 注 · 扣除佣金后</span><dl><dt>投注金额</dt><dd>{{money(p.value.stake_cents)}}</dd><dt>派彩（含本金）</dt><dd>{{money(p.value.payout_cents)}}</dd><dt>佣金成本</dt><dd>{{money(p.value.commission_cents)}}</dd></dl></article><article class="surface pending-metric"><small>当前总押注中金额</small><strong>{{money(data.pending_amount_cents)}}</strong><span>{{data.pending_orders}} 注等待结算</span><p>{{data.reset_at?'仅包含重置后新增的待结算订单；重置前的订单仍正常结算。':'包含待开奖、待自动结算及待人工结算订单；不计入已结算盈亏。'}}</p></article></div>
<section class="surface control-explanation"><h3>统计口径</h3><p>盈亏＝已结算投注金额－含本金派彩－该批订单产生的佣金。正数为平台盈利，负数为平台亏损，按结算时间归入北京时间自然日或自然月；佣金无论是否领取均计入成本。</p><p>退款订单不计盈亏；和局退本净额为0。充值、提现、注册赠送和管理员调账不计入投注盈亏。统计依据账户内投注与结算记录，不等同于实际资金收支或财务净利润。</p><p v-if="data.undated_settled_orders" class="negative">有 {{data.undated_settled_orders}} 笔已结算订单缺少结算时间，未计入日期盈亏，请在订单管理中核对。</p></section>
<section class="surface sessions-panel">
 <div class="control-toolbar"><div><h3>场次状态与号码来源</h3><p>预生成号码仅管理员可见 · 数据每5秒同步 · 倒计时每秒更新 · 北京时间</p></div><div class="control-actions"><button class="secondary-button" @click="emit('settings')"><SlidersHorizontal :size="15"/>接单与限额</button><button class="secondary-button" @click="emit('odds')">赔率设置</button></div></div>
 <p class="estimate-note">预计盈亏＝下一期当前已下注金额－预计派彩（含本金）－佣金；正数为平台盈利，随新增或撤销订单更新。人工结算订单无法完整预估。</p>
 <div class="data-table-wrap"><table class="data-table session-table">
  <thead><tr><th>游戏场次</th><th>上期开奖号码</th><th>下一期开奖号码</th><th>下期开奖 / 倒计时</th><th>预计下期盈亏</th><th>接单状态</th><th>单用户单期上限</th><th>今日盈亏</th><th>待结算金额 / 注数</th><th>号码源 / 位置</th></tr></thead>
  <tbody><tr v-for="s in data.sessions" :key="s.session_id">
   <td>{{s.name}}</td>
   <td class="session-draw"><template v-if="s.latest"><span class="plain-numbers">{{numbers(s.latest)}}</span><small>{{s.latest.issue}} 期</small></template><span v-else class="draw-muted">暂无开奖记录</span></td>
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
<AdminUserStatus :draw-revision="drawRevision" @draw-changed="drawChanged"/>
<Teleport to="body">
 <dialog ref="resetDialog" class="control-reset-dialog" aria-labelledby="reset-records-title" aria-describedby="reset-records-description" @cancel="resetBusy&&$event.preventDefault()" @close="resetReturnFocus?.focus()" @click="resetBackdrop">
  <h2 id="reset-records-title">确认重置记录？</h2>
  <div id="reset-records-description"><p>将清零顶部4张卡片：今日盈亏、昨日盈亏、本月盈亏、当前总押注中金额，包括注数、投注金额、派彩和佣金统计。</p><p>重置后仅统计新提交的订单。已有订单、账户余额、冻结资金、佣金及开奖记录不会被删除或修改；重置前的待结算订单仍正常结算，但不再计入这4项统计。</p><p>此次操作对所有管理员生效，并记录到操作日志。</p></div>
  <p v-if="resetError" class="form-error" role="alert">{{resetError}}。可重试确认，同一次重试不会再次重置。</p>
  <footer><button class="secondary-button" autofocus :disabled="resetBusy" @click="closeReset">取消</button><button class="primary-button" :disabled="resetBusy" @click="resetRecords">{{resetBusy?'正在重置…':'确认重置'}}</button></footer>
 </dialog>
</Teleport>
<Teleport to="body"><div v-if="selected" class="modal-overlay random-overlay" @click.self="closeRandom" @keydown.esc.stop="closeRandom" @keydown="trapDialog">
 <section ref="dialog" class="modal random-dialog" role="dialog" aria-modal="true" aria-labelledby="random-title" tabindex="-1">
  <header class="random-header">
   <button class="modal-close icon-button" :disabled="applying" aria-label="关闭随机试算" @click="closeRandom"><X :size="20"/></button>
   <h2 id="random-title">下一期随机试算</h2>
   <p class="random-meta"><strong>{{selected.name}}</strong><span>第 {{selected.issue}} 期</span></p>
   <p class="random-timing"><span>开奖 {{formatDrawTime(selected.draw_at)}}</span><span>操作截止 {{datetime(new Date(cutoff(selected.draw_at)).toISOString())}}</span></p>
  </header>
  <div class="random-body">
   <p class="random-note">选择一组后确认采用；随机5次可对比5组结果。盈亏随订单更新，正数为平台盈利。</p>
   <p v-if="locked" class="form-error" role="status">{{cutoffMessage(selected.draw_at)}}</p>
   <p v-else-if="previewError" class="form-error" role="alert">{{previewError}}</p>
   <p v-if="!preview&&previewBusy" class="loading-line">正在读取本期号码与押注…</p>
   <table v-if="preview" class="random-comparison" aria-label="当前号码与随机结果对比">
    <thead><tr><th scope="col">结果 / 选择</th><th scope="col">号码</th><th scope="col">当前押注</th><th scope="col">预计派彩</th><th scope="col">预计盈亏</th></tr></thead>
    <tbody><tr v-for="item in comparisonItems" :key="item.id" class="random-result" :class="{'random-result-selected':selectedCandidateId===item.id,'random-result-current':item.id==='current'}">
     <th scope="row" class="random-result-label">
      <label v-if="item.id!=='current'&&item.id!=='empty'" class="random-choice"><input v-model="selectedCandidateId" type="radio" name="random-candidate" :value="item.id" :disabled="locked||previewBusy||applying" :aria-label="`选择${item.label}`"><span>{{item.label}}</span></label>
      <span v-else>{{item.label}}</span>
     </th>
     <td class="random-numbers">{{item.round?numbers(item.round):'点击随机生成结果'}}</td>
     <td class="random-amount" data-label="当前押注"><template v-if="item.estimate">{{money(item.estimate.stake_cents)}}<small>{{item.estimate.orders}} 注</small></template><span v-else>—</span></td>
     <td class="random-amount" data-label="预计派彩">{{item.estimate?money(item.estimate.payout_cents):'—'}}</td>
     <td class="random-amount random-profit" data-label="预计盈亏"><template v-if="item.estimate"><strong :class="{positive:(item.estimate.net_profit_cents??0)>0,negative:(item.estimate.net_profit_cents??0)<0}">{{item.estimate.net_profit_cents===null?'无法完整预估':signed(item.estimate.net_profit_cents)}}</strong><small v-if="item.estimate.unresolved_orders" class="draw-pending">{{item.estimate.unresolved_orders}} 注待人工判定</small></template><span v-else>—</span></td>
    </tr></tbody>
   </table>
   <p v-if="comparisonItems.some(item=>item.estimate?.unresolved_orders)" class="random-note">需人工判定的订单未计入预计派彩。</p>
  </div>
  <footer class="random-footer">
   <p class="random-reminder">确认后替换本期号码，请在操作截止前完成。</p>
   <div class="random-actions"><button class="secondary-button" :disabled="applying" @click="closeRandom">关闭</button><button class="secondary-button" :disabled="locked||previewBusy||applying||!preview" @click="updatePreview(true)"><Shuffle :size="14"/>{{randomCount===1?'试算中…':'随机'}}</button><button class="secondary-button" :disabled="locked||previewBusy||applying||!preview" @click="updatePreview(true,5)"><Shuffle :size="14"/>{{randomCount===5?'正在生成5组…':'随机5次'}}</button><button class="primary-button" :disabled="locked||previewBusy||applying||!!previewError||!selectedCandidate" @click="applyRandom">{{applying?'正在采用…':'确认采用'}}</button></div>
  </footer>
 </section>
</div></Teleport>
</div></template>
<style scoped>
.next-number-line{display:flex;align-items:center;gap:8px}.random-button{display:inline-flex;align-items:center;gap:3px;border:1px solid #e6e2ee;border-radius:4px;padding:2px 5px;font-size:10px;color:#8063b6;white-space:nowrap}.saved-message{font-size:12px;color:#399b7b;margin:0 0 14px}
.random-overlay{padding:16px}
.random-dialog{display:flex;flex-direction:column;width:900px;max-width:100%;max-height:calc(100dvh - 32px);overflow:hidden;padding:0;border-radius:16px;text-align:left}
.random-header{flex-shrink:0;padding:18px 22px 12px;border-bottom:1px solid #eeebf3}
.random-dialog h2{font-size:18px;line-height:1.4;margin:0 32px 8px 0}
.random-dialog .random-meta,.random-dialog .random-timing{display:flex;flex-wrap:wrap;gap:4px 16px;font-size:12px;line-height:1.5;margin:4px 0;color:#8e849e}
.random-meta strong{font-weight:600;color:#625970}
.random-dialog .random-timing{font-size:11px}
.random-body{min-height:0;overflow:auto;overscroll-behavior:contain;padding:0 22px 12px}
.random-dialog .random-note{font-size:11px;line-height:1.5;margin:10px 0;color:#8d809b}
.random-dialog .form-error{padding:7px 10px;font-size:12px;line-height:1.5;margin:8px 0;color:#bf4e57;border-radius:6px}
.random-comparison{width:100%;border-collapse:separate;border-spacing:0;table-layout:fixed;font-size:12px;color:#6e647d;border:1px solid #e9e4f0;border-radius:8px;overflow:hidden}
.random-comparison th,.random-comparison td{padding:10px 12px;line-height:1.5;border-bottom:1px solid #eeeaf4;vertical-align:middle;overflow-wrap:anywhere}
.random-comparison thead th{font-size:11px;font-weight:500;color:#998aa9;background:#faf8fd;text-align:right}
.random-comparison thead th:first-child{width:120px;text-align:left}
.random-comparison thead th:nth-child(2){width:32%;text-align:left}
.random-comparison tbody tr:last-child>*{border-bottom:0}
.random-result-label{text-align:left;font-weight:500}
.random-result-current{background:#faf9fc}
.random-result-selected{background:#f2edfc}
.random-result-selected .random-result-label{color:#795cbb;box-shadow:inset 3px 0 #9071cd}
.random-choice{display:flex;align-items:center;gap:7px;cursor:pointer;min-height:24px}
.random-choice input{accent-color:#8063b6;width:15px;height:15px;margin:0;flex-shrink:0}
.random-numbers{font-size:14px;font-weight:600;font-variant-numeric:tabular-nums;color:#53586c}
.random-amount{text-align:right;font-variant-numeric:tabular-nums}
.random-amount small{display:block;font-size:10px;font-weight:400;color:#a092b1;line-height:1.4}
.random-profit strong{font-size:13px}
.random-footer{flex-shrink:0;display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:8px 16px;padding:12px 22px;border-top:1px solid #eeebf3;background:#fff}
.random-dialog .random-reminder{font-size:11px;line-height:1.5;margin:0;color:#a57428}
.random-actions{display:flex;flex-wrap:wrap;justify-content:flex-end;gap:8px;margin-left:auto}
.random-actions button{font-size:12px;padding:8px 12px;white-space:nowrap}
.random-dialog button:disabled,.random-choice input:disabled{opacity:.5;cursor:not-allowed}
.random-choice:has(input:disabled){cursor:default}
@media(min-width:681px) and (max-height:640px){
 .random-header{padding:12px 18px 8px}.random-dialog h2{font-size:17px;margin-bottom:5px}
 .random-body{padding:0 18px 8px}.random-dialog .random-note{margin:8px 0}
 .random-comparison th,.random-comparison td{padding:6px 10px}.random-comparison thead th:first-child{width:110px}
 .random-numbers{font-size:13px}.random-footer{padding:8px 18px}
}
@media(max-width:680px){
 .random-overlay{padding:10px}.random-dialog{max-height:calc(100dvh - 20px);border-radius:12px}
 .random-header{padding:14px 14px 10px}.random-body{padding:0 14px 10px}.random-footer{padding:10px 14px}
 .random-comparison{display:block;border:0;overflow:visible}.random-comparison thead{position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%)}
 .random-comparison tbody{display:grid;gap:6px}.random-result{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));border:1px solid #e9e4f0;border-radius:7px;padding:8px 10px;gap:6px 10px}
 .random-comparison th,.random-comparison td{border:0;padding:0}.random-result-label{grid-column:1/-1}.random-choice{min-height:20px}
 .random-comparison .random-numbers{grid-column:1/-1;font-size:13px}.random-amount{text-align:left;font-size:12px}
 .random-amount::before{content:attr(data-label);display:block;font-size:10px;color:#a092b1;margin-bottom:2px}.random-amount small{display:inline;margin-left:4px}.random-profit small{display:block;margin-left:0}
 .random-result-selected{border-color:#ab93d2}.random-result-selected .random-result-label{box-shadow:none}
 .random-actions{gap:6px}.random-actions button{padding:8px 10px}
}

.sessions-panel{padding:18px;margin-bottom:20px}.sessions-panel .control-toolbar{margin-bottom:10px}.estimate-note{font-size:10px;line-height:1.7;color:#9b8aaa;margin-bottom:12px}.session-table{font-size:11px}.session-table th,.session-table td{padding:9px 10px;line-height:1.45}.session-table td small{font-size:10px;margin-top:3px}.plain-numbers{font-size:12px;font-weight:500;font-variant-numeric:tabular-nums;color:#53586c}.session-next{min-width:125px}.draw-countdown{font-size:12px;font-weight:600;font-variant-numeric:tabular-nums;color:#62677e}.session-estimate strong{font-size:12px;font-variant-numeric:tabular-nums}.session-table .badge{padding:3px 6px;font-size:10px}.draw-muted{color:#a092b1}.draw-pending{color:#b58637!important}

.control-toolbar{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:20px}.control-toolbar h2{font-size:20px}.control-toolbar p{font-size:11px;color:#a092b1;margin-top:8px;line-height:1.8}.control-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:18px;margin-bottom:20px}.control-metrics article{padding:24px;min-width:0}.control-metrics small{font-size:12px;color:#9989a8}.control-metrics strong{display:block;font-size:26px;color:#8063b6;margin:16px 0 9px;overflow-wrap:anywhere;font-variant-numeric:tabular-nums}.control-metrics span,.pending-metric p{font-size:11px;color:#a295ae;line-height:1.9}.pending-metric p{margin-top:24px}.control-metrics dl{display:grid;grid-template-columns:1fr auto;gap:12px 6px;border-top:1px solid #efedf5;margin:20px 0 0;padding-top:18px;font-size:11px;color:#9887a8}.control-metrics dd{margin:0;font-variant-numeric:tabular-nums}.control-explanation{margin-bottom:20px}.control-explanation p{font-size:12px;color:#9b8aaa;line-height:1.9;margin-top:10px}.control-actions{display:flex;gap:10px;flex-shrink:0}.positive{color:#399b7b!important}.negative{color:#c76878!important}@media(max-width:1200px){.control-metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:640px){.control-metrics{gap:10px}.control-metrics article{padding:16px 12px}.control-metrics strong{font-size:22px}.control-metrics dl{grid-template-columns:1fr;gap:5px}.control-metrics dd{margin-bottom:6px}.control-toolbar{flex-wrap:wrap}.control-actions{width:100%;flex-wrap:wrap}.control-actions button{font-size:11px}.control-explanation{padding:18px}}
</style>

<style scoped>
.control-reset-note{font-size:12px;color:#927caf;line-height:1.7;margin:-8px 0 20px}
.control-reset-dialog{border:1px solid #ebe5f6;border-radius:20px;padding:26px;width:min(500px,calc(100vw - 32px));max-height:85dvh;overflow:auto;color:#676079;background:white;box-shadow:0 24px 80px #39284b30}
.control-reset-dialog::backdrop{background:#2c213f66;backdrop-filter:blur(3px)}
.control-reset-dialog h2{font-size:20px;color:#302b42;margin:0 0 16px}
.control-reset-dialog p{font-size:13px;line-height:1.8;margin:12px 0}
.control-reset-dialog footer{display:flex;justify-content:flex-end;gap:10px;margin-top:24px}
.control-reset-dialog button:disabled{opacity:.5;cursor:not-allowed}
@media(max-width:640px){.control-reset-dialog{padding:22px}.control-reset-note{font-size:11px}}
</style>
