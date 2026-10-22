<script setup lang="ts">
import { formatIssue } from '../lib/issueDisplay'
import {computed,nextTick,onUnmounted,ref,watch} from 'vue'
import {Check,ChevronRight,RefreshCw,Trash2,X} from 'lucide-vue-next'
import {money,refreshAccount,user} from '../api/account'
import {oddsText,type Market,type MarketBook} from '../api/markets'
import {parseStake} from '../lib/stake'
import StakeAmount from './StakeAmount.vue'

const props=defineProps<{
 book:MarketBook;selections:{market:Market;odds:number}[];entryMode:'quick'|'general';amount:string;selectionAmounts:Record<string,string>;
 locked:boolean;busy:boolean;blocked:boolean;retrying:boolean;issue:string;min:number;max:number;total:number;submitDisabled:boolean;notice:string;canConfirmQuotes:boolean
}>()
const emit=defineEmits<{
 select:[market:Market];remove:[id:string];clear:[];mode:[value:'quick'|'general'];amount:[value:string];'selection-amount':[market:Market,value:string];
 submit:[];'confirm-quotes':[];refresh:[];login:[];notify:[message:string]
}>()
const category=ref<'sides'|'numbers'|'sum'>('sides')
const categories=[{id:'sides' as const,label:'双面盘'},{id:'numbers' as const,label:'单号1-10'},{id:'sum' as const,label:'冠亚军'}]
const quotes=computed(()=>Object.fromEntries(props.book.quotes.map(q=>[q.market_id,q])))
const selectedIDs=computed(()=>new Set(props.selections.map(s=>s.market.id)))
const displayGroup=(group:string)=>group.replace(/^第(\d+)名$/,(_,rank:string)=>['冠军','亚军','第三名','第四名','第五名','第六名','第七名','第八名','第九名','第十名'][Number(rank)-1]??group)
const isSum=(m:Market)=>m.group==='冠亚和'&&m.positions.length===2&&m.positions[0]===1&&m.positions[1]===2
const combo=(m:Market)=>m.id.startsWith('sum.set_')
const optionLabel=(m:Market)=>combo(m)?m.pick:isSum(m)&&m.kind==='number'?m.label.padStart(2,'0'):isSum(m)&&['size','parity'].includes(m.kind)&&!m.label.startsWith('和')?`和${m.label}`:m.label
const groups=computed(()=>{
 const all=props.book.markets
 if(category.value==='sum'){
  const sum=all.filter(isSum)
  return [
   {name:'冠亚军和',items:sum.filter(m=>m.kind==='number').sort((a,b)=>Number(a.pick)-Number(b.pick))},
   {name:'冠亚军和 · 大小单双',items:sum.filter(m=>['size','parity'].includes(m.kind))},
   {name:'冠亚军和 · 组合',items:sum.filter(m=>combo(m)||!['number','size','parity'].includes(m.kind))},
  ].filter(g=>g.items.length)
 }
 const visible=all.filter(m=>category.value==='numbers'
  ? /^p\d+\.n\d+$/.test(m.id)&&m.kind==='number'
  : m.category==='two_sides'&&!isSum(m)&&(m.kind!=='compare'||m.positions.length===2&&m.positions[0]===1&&m.positions[1]===10))
 return [...new Set(visible.map(m=>m.group))].map(name=>({name:displayGroup(name),items:visible.filter(m=>m.group===name)}))
})
const entryDescription=computed(()=>props.entryMode==='quick'?'快捷模式：每个选项使用相同的每注金额':'一般模式：输入金额即可选中，可为每项填写不同金额')
const balanceRefreshing=ref(false),confirmOpen=ref(false),dialog=ref<HTMLElement|null>(null),content=ref<HTMLElement|null>(null)
let returnFocus:HTMLElement|null=null,priorOverflow:string|null=null
async function refreshBalance(){
 if(balanceRefreshing.value)return
 if(!user.value){emit('login');return}
 balanceRefreshing.value=true
 try{await refreshAccount();emit('notify','余额已刷新')}catch(e){emit('notify',(e as Error).message)}finally{balanceRefreshing.value=false}
}
function chooseCategory(next:typeof category.value){if(!props.locked){category.value=next;if(content.value)content.value.scrollTop=0}}
function inputAmount(m:Market,event:Event){emit('selection-amount',m,(event.target as HTMLInputElement).value)}
const selectionCost=(id:string)=>props.entryMode==='general'?parseStake(props.selectionAmounts[id]??''):parseStake(props.amount)
function openConfirmation(event:MouseEvent){
 if(props.busy)return
 if(!props.selections.length){emit('notify','请先选择投注项');return}
 returnFocus=event.currentTarget as HTMLElement;confirmOpen.value=true
}
function closeConfirmation(){if(!props.busy)confirmOpen.value=false}
function submitConfirmation(){if(!user.value)confirmOpen.value=false;emit('submit')}
function clear(){if(props.locked)return;confirmOpen.value=false;emit('clear')}
function trap(event:KeyboardEvent){
 if(event.key!=='Tab')return
 const nodes=dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),[tabindex="0"]')
 if(!nodes?.length){event.preventDefault();return}
 const first=nodes[0]!,last=nodes[nodes.length-1]!,outside=!dialog.value?.contains(document.activeElement)
 if(event.shiftKey&&(outside||document.activeElement===first||document.activeElement===dialog.value)){event.preventDefault();last.focus()}
 else if(!event.shiftKey&&(outside||document.activeElement===last||document.activeElement===dialog.value)){event.preventDefault();first.focus()}
}
function restoreOverflow(){if(priorOverflow!==null){document.body.style.overflow=priorOverflow;priorOverflow=null}}
watch(confirmOpen,async open=>{
 if(open){priorOverflow=document.body.style.overflow;document.body.style.overflow='hidden';await nextTick();dialog.value?.focus()}
 else{restoreOverflow();if(returnFocus?.isConnected)returnFocus.focus()}
})
watch(()=>props.selections.length,count=>{if(!count&&props.busy)confirmOpen.value=false})
watch(()=>props.issue,()=>{if(!props.locked)confirmOpen.value=false})
watch(()=>user.value?.id,(next,previous)=>{if(previous&&next!==previous)confirmOpen.value=false},{flush:'sync'})
onUnmounted(restoreOverflow)
</script>

<template>
 <div class="racing-board">
  <div class="racing-controls">
   <div class="racing-balance"><span>余额：<b>{{user?money(user.balance_cents):'—'}}</b></span><button :disabled="balanceRefreshing" aria-label="刷新余额" @click="refreshBalance"><RefreshCw :size="14" :class="{spinning:balanceRefreshing}"/></button></div>
   <div class="racing-entry-mode" aria-label="投注金额模式"><button :class="{active:entryMode==='quick'}" :aria-pressed="entryMode==='quick'" :disabled="locked" @click="emit('mode','quick')">快捷</button><button :class="{active:entryMode==='general'}" :aria-pressed="entryMode==='general'" :disabled="locked" @click="emit('mode','general')">一般</button></div>
  </div>
  <div class="racing-layout">
   <nav class="racing-menu" aria-label="PK10玩法分类"><button v-for="item in categories" :key="item.id" :class="{active:category===item.id}" :aria-pressed="category===item.id" :disabled="locked" @click="chooseCategory(item.id)">{{item.label}}<ChevronRight :size="13"/></button></nav>
   <div ref="content" class="racing-content">
    <div class="racing-category-note"><span>{{entryDescription}}</span><button :disabled="locked" aria-label="刷新赔率" @click="emit('refresh')"><RefreshCw :size="13"/></button></div>
    <section v-for="group in groups" :key="group.name" class="racing-group">
     <h3>{{group.name}}</h3>
     <div class="racing-options" :class="{'general-amounts':entryMode==='general'}">
      <div v-for="m in group.items" :key="m.id" class="racing-option" :class="{selected:selectedIDs.has(m.id),combo:combo(m),paused:!quotes[m.id]?.enabled}">
       <button class="racing-option-pick" :disabled="locked||!quotes[m.id]?.enabled" :aria-label="`${m.group}${optionLabel(m)}，赔率${oddsText(quotes[m.id]?.odds_milli??0)}`" :aria-pressed="selectedIDs.has(m.id)" @click="emit('select',m)"><strong :class="{numeric:m.kind==='number'}">{{optionLabel(m)}}</strong><span>{{quotes[m.id]?.enabled?oddsText(quotes[m.id]!.odds_milli):'已暂停'}}</span><Check v-if="selectedIDs.has(m.id)" :size="13"/></button>
       <input v-if="entryMode==='general'" :value="selectionAmounts[m.id]??''" :disabled="locked||!quotes[m.id]?.enabled" inputmode="decimal" :aria-label="`${m.group}${optionLabel(m)}金额（元）`" placeholder="金额" @input="inputAmount(m,$event)"/>
      </div>
     </div>
    </section>
    <p v-if="!groups.length" class="racing-empty">当前分类暂无可用玩法</p>
   </div>
  </div>
  <div class="racing-bet-dock">
   <button class="racing-clear" :disabled="locked||!selections.length" aria-label="清空全部投注" @click="clear"><Trash2 :size="21"/></button>
   <div class="racing-total"><span>共 <b>{{selections.length}}</b> 注</span><strong>{{money(total)}}<small> 元</small></strong></div>
   <button class="racing-place" :disabled="busy||!selections.length||(blocked&&!retrying)" @click="openConfirmation">{{busy?'提交中…':retrying?'重试确认':'立即投注'}}</button>
   <p v-if="notice" class="racing-dock-notice" role="status">{{notice}}<button v-if="canConfirmQuotes" :disabled="locked" @click="emit('confirm-quotes')">确认最新赔率</button></p>
  </div>
  <Teleport to="body">
   <div v-if="confirmOpen" class="modal-overlay racing-confirm-overlay" @click.self="closeConfirmation" @keydown.esc.stop="closeConfirmation" @keydown="trap">
    <section ref="dialog" class="racing-confirm" role="dialog" aria-modal="true" aria-labelledby="racing-confirm-title" tabindex="-1">
     <header><div><h2 id="racing-confirm-title">确认投注</h2><p>第 {{formatIssue(issue)}} 期 · {{entryMode==='quick'?'快捷模式':'一般模式'}}</p></div><button :disabled="busy" aria-label="关闭投注确认" @click="closeConfirmation"><X :size="20"/></button></header>
     <div class="racing-confirm-body">
      <StakeAmount v-if="entryMode==='quick'" :model-value="amount" label="每注金额" :min="min" :max="max" :disabled="locked" @update:model-value="emit('amount',$event)"/>
      <p v-else class="racing-amount-hint">每项金额 {{money(min)}}–{{money(max)}} 元，分别结算。</p>
      <div class="racing-confirm-selections" aria-label="已选投注">
       <article v-for="s in selections" :key="s.market.id"><div><strong>{{displayGroup(s.market.group)}} · {{optionLabel(s.market)}}</strong><small>赔率 {{oddsText(s.odds)}} · 中奖返还 {{money(Math.floor(selectionCost(s.market.id)*s.odds/1000))}} 元</small></div><input v-if="entryMode==='general'" :value="selectionAmounts[s.market.id]??''" :disabled="locked" inputmode="decimal" :aria-label="`已选${s.market.group}${optionLabel(s.market)}金额（元）`" @input="inputAmount(s.market,$event)"/><span v-else>{{money(selectionCost(s.market.id))}} 元</span><button :disabled="locked" :aria-label="`移除${s.market.group}${optionLabel(s.market)}`" @click="emit('remove',s.market.id)"><X :size="15"/></button></article>
      </div>
      <p v-if="!selections.length" class="racing-empty">尚未选择投注项</p>
      <p v-if="notice" class="racing-confirm-notice" role="status">{{notice}}<button v-if="canConfirmQuotes" :disabled="locked" @click="emit('confirm-quotes')">确认最新赔率</button></p>
     </div>
     <footer><div><span>共 {{selections.length}} 注</span><strong>¥ {{money(total)}}</strong></div><button class="racing-place" :disabled="submitDisabled" @click="submitConfirmation">{{busy?'提交中…':retrying?'重试确认':!user?'登录后投注':'确认投注'}}</button></footer>
    </section>
   </div>
  </Teleport>
 </div>
</template>

<style scoped>
.racing-board{display:flex;flex-direction:column;flex:1;min-height:0;overflow:hidden;background:#fff;color:#484354}.racing-controls{flex-shrink:0;display:flex;align-items:center;justify-content:space-between;gap:10px;min-height:44px;padding:7px 12px;border-bottom:1px solid #e9e7ed;background:#fff}.racing-balance{display:flex;align-items:center;gap:5px;font-size:12px}.racing-balance b{font-weight:500;font-variant-numeric:tabular-nums;color:#7960b4}.racing-balance button{display:grid;place-items:center;width:28px;height:28px;color:#8c849b}.racing-entry-mode{display:flex;gap:0;border:1px solid #8f6ae3;border-radius:5px;overflow:hidden}.racing-entry-mode button{min-width:54px;padding:5px 12px;font-size:12px;color:#8664cf;background:#fff}.racing-entry-mode button.active{background:linear-gradient(110deg,#6575ef,#9860e9);color:#fff}.racing-layout{display:grid;grid-template-columns:120px minmax(0,1fr);flex:1;min-height:0;overflow:hidden}.racing-menu{background:#f4f4f7;border-right:1px solid #e7e5ec}.racing-menu button{display:flex;align-items:center;justify-content:space-between;gap:4px;width:100%;min-height:54px;padding:12px 10px;border-bottom:1px solid #e4e2e9;background:#f9f9fb;color:#625b70;font-size:13px}.racing-menu button.active{color:#fff;background:linear-gradient(110deg,#536beb,#9860e9)}.racing-content{min-width:0;min-height:0;overflow-y:auto;overscroll-behavior:contain}.racing-category-note{display:flex;align-items:center;justify-content:space-between;gap:5px;padding:6px 12px;background:#fbfafc;color:#a099ae;font-size:10px;line-height:1.5;min-height:30px;border-bottom:1px solid #ece9f1}.racing-category-note button{display:grid;place-items:center;width:25px;height:25px;flex-shrink:0;color:#8d7baa}.racing-group h3{font-size:12px;font-weight:500;padding:8px 12px;margin:0;background:#f7f6f9;border-bottom:1px solid #e6e3ed;color:#8c6b8b}.racing-options{display:grid;grid-template-columns:repeat(2,minmax(0,1fr))}.racing-option{position:relative;min-width:0;border-right:1px solid #e7e5ec;border-bottom:1px solid #e7e5ec;background:#fff}.racing-option.combo{grid-column:1/-1}.racing-option-pick{display:flex;align-items:center;justify-content:center;gap:24px;width:100%;min-height:47px;padding:8px 20px 8px 10px;text-align:center}.racing-option-pick strong{font-size:16px;line-height:1.3;font-weight:500;overflow-wrap:anywhere}.racing-option-pick span{font-size:12px;line-height:1.2;color:#b07985;font-variant-numeric:tabular-nums}.racing-option-pick>svg{position:absolute;right:5px;top:5px;color:#9770da}.racing-option.selected{background:#f3eefd;box-shadow:inset 0 0 0 1px #b69bdf}.racing-option.selected .racing-option-pick strong{color:#8f62cf}.racing-option.paused{opacity:.45}.racing-option>input{display:block;width:calc(100% - 16px);min-width:0;height:29px;margin:0 8px 8px;padding:4px 6px;border:1px solid #e0dce8;border-radius:4px;background:#fff;font-size:12px;text-align:center;color:#625172;outline:none}.racing-option>input:focus{border-color:#9f7bda;box-shadow:0 0 0 2px #9f7bda20}.general-amounts .racing-option-pick{min-height:39px;padding-bottom:5px}.racing-empty{padding:28px 12px;text-align:center;font-size:12px;color:#aaa0b6}.racing-bet-dock{position:relative;flex-shrink:0;z-index:1;display:flex;align-items:center;gap:12px;flex-wrap:wrap;min-height:66px;padding:10px 20px calc(10px + env(safe-area-inset-bottom,0px));border-top:1px solid #ece9f2;background:#fff;box-shadow:0 -2px 10px #342b4310}.racing-clear{display:grid;place-items:center;width:38px;height:36px;border-right:1px solid #e6e1ec;color:#aa9aba}.racing-total{display:flex;align-items:center;gap:18px;flex:1;font-size:13px}.racing-total b{color:#b286d5;font-weight:500}.racing-total strong{font-size:16px;font-weight:500;color:#9672bb;font-variant-numeric:tabular-nums}.racing-total small{font-size:11px;font-weight:400}.racing-place{min-width:140px;min-height:40px;padding:10px 24px;border-radius:5px;background:linear-gradient(110deg,#546dee,#9a5de7);font-size:14px;color:#fff;font-weight:500;white-space:nowrap}.racing-dock-notice{width:100%;margin:0;color:#ab7b71;font-size:10px;line-height:1.5}.racing-dock-notice button,.racing-confirm-notice button{margin-left:8px;text-decoration:underline;color:#8963b6}.racing-board button:disabled,.racing-confirm button:disabled,.racing-confirm input:disabled,.racing-board input:disabled{opacity:.5;cursor:not-allowed}.spinning{animation:racing-spin 1s linear infinite}@keyframes racing-spin{to{transform:rotate(360deg)}}
.racing-confirm-overlay{z-index:65;padding:16px}.racing-confirm{display:flex;flex-direction:column;width:640px;max-width:100%;max-height:calc(100dvh - 32px);overflow:hidden;border-radius:14px;background:#fff;color:#62556f;box-shadow:0 12px 45px #29203424;outline:none}.racing-confirm>header{display:flex;align-items:flex-start;justify-content:space-between;padding:17px 18px;border-bottom:1px solid #ebe6f1;gap:12px}.racing-confirm h2{font-size:18px;margin:0 0 6px;color:#4b3f5b}.racing-confirm header p{margin:0;font-size:11px;color:#9a8ca7}.racing-confirm header button{display:grid;place-items:center;width:26px;height:26px;color:#9a8ca7}.racing-confirm-body{min-height:0;overflow:auto;padding:14px 18px}.racing-confirm-body :deep(.stake-control){margin:0 0 12px;padding:0 0 14px;border:0;border-bottom:1px solid #eee8f5;border-radius:0}.racing-confirm-body :deep(.stake-heading){gap:10px;flex-wrap:wrap}.racing-confirm-body :deep(.stake-heading label){font-size:12px}.racing-confirm-body :deep(.stake-heading input){width:115px}.racing-confirm-body :deep(.stake-shortcuts){gap:6px}.racing-confirm-body :deep(.stake-shortcuts button){min-width:0}.racing-confirm-selections{display:grid;gap:7px}.racing-confirm-selections article{display:flex;align-items:center;gap:8px;padding:10px;border:1px solid #e9e2f1;border-radius:6px;background:#faf8fe}.racing-confirm-selections article>div{flex:1;min-width:0}.racing-confirm-selections strong{display:block;font-size:12px;line-height:1.5;overflow-wrap:anywhere}.racing-confirm-selections small{display:block;margin-top:4px;font-size:10px;line-height:1.5;color:#a092af}.racing-confirm-selections article>span{font-size:11px;white-space:nowrap}.racing-confirm-selections input{width:80px;min-width:0;flex-shrink:0;border:1px solid #ddd3e9;border-radius:4px;background:#fff;padding:7px 5px;text-align:center;font-size:12px;color:#685276}.racing-confirm-selections article>button{display:grid;place-items:center;width:23px;height:25px;flex-shrink:0;color:#a493ae}.racing-amount-hint{font-size:11px;color:#a08ca8;line-height:1.7;margin:0 0 12px}.racing-confirm-notice{padding:9px 10px;font-size:11px;line-height:1.7;color:#b78174;background:#fff8f2;border-radius:5px;margin:12px 0 0}.racing-confirm>footer{display:flex;justify-content:space-between;align-items:center;gap:12px;padding:12px 18px;border-top:1px solid #ebe6f1}.racing-confirm>footer>div{display:flex;align-items:center;gap:10px;font-size:12px}.racing-confirm>footer strong{font-size:17px;color:#9873ba;font-weight:500;white-space:nowrap}
@media(max-width:760px){.racing-controls{padding:6px 9px;min-height:39px}.racing-balance{font-size:11px}.racing-entry-mode button{min-width:46px;padding:4px 10px;font-size:11px}.racing-layout{grid-template-columns:78px minmax(0,1fr)}.racing-menu button{min-height:47px;padding:11px 7px;font-size:11px}.racing-category-note{font-size:9px;padding:4px 7px}.racing-group h3{padding:7px 9px;font-size:11px}.racing-option-pick{min-height:43px;padding:8px 15px 8px 6px;gap:16px}.racing-option-pick strong{font-size:15px}.racing-option-pick span{font-size:11px}.racing-option.combo .racing-option-pick{justify-content:space-between;padding-left:16px;padding-right:28px;gap:8px}.racing-option.combo .racing-option-pick strong{font-size:13px}.racing-bet-dock{gap:8px;padding:8px 10px calc(8px + env(safe-area-inset-bottom,0px));min-height:61px}.racing-clear{width:29px}.racing-total{gap:8px;font-size:11px}.racing-total strong{font-size:14px}.racing-total small{font-size:10px}.racing-place{min-width:110px;padding:10px 14px;min-height:39px;font-size:13px}.racing-confirm-overlay{padding:10px}.racing-confirm{max-height:calc(100dvh - 20px);border-radius:12px}.racing-confirm>header{padding:14px}.racing-confirm-body{padding:12px 14px}.racing-confirm>footer{padding:11px 14px}.racing-confirm>footer>div{display:block;font-size:11px}.racing-confirm>footer strong{display:block;margin-top:3px;font-size:16px}.racing-confirm-selections input{width:64px}.racing-confirm-body :deep(.stake-shortcuts){grid-template-columns:repeat(3,minmax(0,1fr))}}
@media(prefers-reduced-motion:reduce){.spinning{animation:none}}
</style>
