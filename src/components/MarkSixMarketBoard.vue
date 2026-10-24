<script setup lang="ts">
import { formatIssue } from '../lib/issueDisplay'
import {computed,nextTick,onUnmounted,ref,watch} from 'vue'
import {Check,ChevronRight,RefreshCw,X} from 'lucide-vue-next'
import {money,user} from '../api/account'
import {type Market,type MarketBook} from '../api/markets'
import {canonicalValues,combinationCount,concreteMarket,expandCombinations,markSixBettingGroups,markSixBettingGroup,quoteID,usesZodiac} from '../lib/marksix'
import {parseStake} from '../lib/stake'
import {marketOddsText,validTierOdds} from '../lib/marketOdds'
import {selectionOdds,conditionalOddsHint} from '../lib/marksixOdds'
import StakeAmount from './StakeAmount.vue'
import MarkSixBall from './MarkSixBall.vue'

const props=defineProps<{book:MarketBook;selections:{market:Market;odds:number;tierOdds?:Record<string,number>}[];amount:string;locked:boolean;busy:boolean;blocked:boolean;retrying:boolean;issue:string;targetIssue:string;min:number;max:number;total:number;submitDisabled:boolean;notice:string;canConfirmQuotes:boolean}>()
const emit=defineEmits<{select:[market:Market];add:[markets:Market[]];remove:[id:string];clear:[];amount:[value:string];submit:[];'confirm-quotes':[];refresh:[];notify:[message:string]}>()
const active=ref('zodiac_tail'),templateID=ref(''),position=ref(''),pool=ref<number[]>([]),pairMode=ref<'numbers'|'zodiac'|'tail'>('numbers'),pairLeft=ref<number[]>([]),pairRight=ref<number[]>([])
const content=ref<HTMLElement|null>(null),dialog=ref<HTMLElement|null>(null),confirmOpen=ref(false),chipsOpen=ref(false)
let returnFocus:HTMLElement|null=null,priorOverflow:string|null=null
const context=computed(()=>props.book.marksix_context)
const contextReady=computed(()=>context.value?.issue===props.targetIssue)
const quotes=computed(()=>Object.fromEntries(props.book.quotes.map(quote=>[quote.market_id,quote])))
const selectedIDs=computed(()=>new Set(props.selections.map(selection=>selection.market.id)))
const uiGroup=markSixBettingGroup
const menus=computed(()=>markSixBettingGroups.filter(group=>props.book.markets.some(market=>uiGroup(market)===group.id)))
type SubmenuItem={id:string;label:string;type:'position'|'template'}
const expandedCategory=ref('')
const menuSections=computed(()=>menus.value.map(menu=>{
 const markets=props.book.markets.filter(market=>uiGroup(market)===menu.id)
 const children:SubmenuItem[]=menu.id==='position_numbers'
  ? [...new Set(markets.map(market=>`正码${market.positions[0]}`))].map(label=>({id:label,label:label.replace('正码','正特码'),type:'position'}))
  : markets.filter(market=>market.selection_schema).map(market=>({id:market.id,label:market.label,type:'template'}))
 return {...menu,children}
}))
const categoryMarkets=computed(()=>props.book.markets.filter(market=>uiGroup(market)===active.value))
const templates=computed(()=>categoryMarkets.value.filter(market=>market.selection_schema))
const template=computed(()=>templates.value.find(market=>market.id===templateID.value)??templates.value[0])
const schema=computed(()=>template.value?.selection_schema)
const positions=computed(()=>active.value==='position_numbers'?[...new Set(categoryMarkets.value.map(market=>`正码${market.positions[0]}`))]:[])
const fixedGroups=computed(()=>{
 const markets=categoryMarkets.value.filter(market=>!market.selection_schema&&(!positions.value.length||`正码${market.positions[0]}`===(position.value||positions.value[0])))
 const names=[...new Set(markets.map(market=>market.group))]
 if(active.value==='zodiac_tail')names.sort((a,b)=>['一肖','正特尾数','正肖'].indexOf(a)-['一肖','正特尾数','正肖'].indexOf(b))
 const groups=names.map(group=>{
  const numbers=markets.filter(market=>market.group===group&&isNumber(market))
  const others=markets.filter(market=>market.group===group&&!isNumber(market))
  return {name:active.value==='position_numbers'?group.replace(/^正码/,'正特码'):group,rows:(active.value==='special'?[numbers,others]:[others,numbers]).filter(rows=>rows.length)}
 })
 if(['special','position_numbers','regular'].includes(active.value))groups.sort((a,b)=>Number(b.rows.some(rows=>rows.every(isNumber)))-Number(a.rows.some(rows=>rows.every(isNumber))))
 return groups
})
const poolOptions=computed(()=>{
 if(schema.value?.type==='zodiac')return context.value?.zodiacs.filter(item=>item.value>=schema.value!.min_value&&item.value<=schema.value!.max_value)??[]
 if(schema.value?.type==='tail')return context.value?.tails.filter(item=>item.value>=schema.value!.min_value&&item.value<=schema.value!.max_value)??[]
 return (context.value?.numbers??[]).filter(item=>!schema.value||item.number>=schema.value.min_value&&item.number<=schema.value.max_value).map(item=>({value:item.number,label:String(item.number).padStart(2,'0'),numbers:[item.number]}))
})
const pairAllowed=computed(()=>active.value==='linked_numbers'&&schema.value?.type==='number'&&schema.value.pick_count===2&&!!template.value&&/二全中|二中特|特串/.test(template.value.label+' '+template.value.group))
const pairOptions=computed(()=>pairMode.value==='zodiac'?context.value?.zodiacs??[]:context.value?.tails??[])
const draftNeedsContext=computed(()=>!!template.value&&(usesZodiac(template.value)||pairMode.value==='zodiac'))
const draftAvailable=computed(()=>!!template.value&&!!quotes.value[template.value.id]?.enabled&&validTierOdds(template.value,quotes.value[template.value.id]?.tier_odds_milli)&&(!draftNeedsContext.value||contextReady.value))
const pairCombinations=computed(()=>{
 if(pairMode.value==='numbers'||!pairAllowed.value)return []
 const numbers=(values:number[])=>canonicalValues(values.flatMap(value=>pairOptions.value.find(item=>item.value===value)?.numbers??[]))
 const pairs=new Map<string,number[]>()
 for(const left of numbers(pairLeft.value))for(const right of numbers(pairRight.value)){if(left===right)continue;const values=canonicalValues([left,right]);pairs.set(values.join(','),values);if(pairs.size>500)return [...pairs.values()]}
 return [...pairs.values()].sort((a,b)=>a[0]!-b[0]!||a[1]!-b[1]!)
})
const draftCount=computed(()=>{
 if(!schema.value)return 0
 if(pairMode.value!=='numbers'&&pairAllowed.value)return pairCombinations.value.length
 if(schema.value.mode==='set')return pool.value.length===schema.value.pick_count?1:0
 return combinationCount(pool.value.length,schema.value.pick_count)
})
const draftMarkets=computed(()=>{
 if(!template.value||!schema.value||draftCount.value>500)return []
 const values=pairMode.value!=='numbers'&&pairAllowed.value?pairCombinations.value:schema.value.mode==='set'?(draftCount.value?[canonicalValues(pool.value)]:[]):expandCombinations(pool.value,schema.value.pick_count)
 return values.map(values=>{const market=concreteMarket(template.value!,values,context.value);return pairMode.value==='zodiac'&&context.value?{...market,selection_context_issue:context.value.issue}:market})
})
const additions=computed(()=>draftMarkets.value.filter(market=>!selectedIDs.value.has(market.id)))
const draftLimitExceeded=computed(()=>draftCount.value>500||props.selections.length+additions.value.length>500)
const amountCost=computed(()=>parseStake(props.amount))
const draftNotice=computed(()=>draftLimitExceeded.value?'本次投注最多500注，请减少选项':draftCount.value&&additions.value.length===0?'这些组合已在投注单中':schema.value?.mode==='set'?`请选择${schema.value.pick_count}个不同生肖，合为1注；只看特码，开49退本`:`每${schema.value?.pick_count??0}项一组，可多选组成复式`)
watch(menus,items=>{if(items.length&&!items.some(item=>item.id===active.value))active.value=items[0]!.id},{immediate:true})
watch(active,()=>{templateID.value='';position.value='';clearDraft();if(content.value)content.value.scrollTop=0})
watch(templateID,clearDraft)
watch(()=>props.targetIssue,()=>{if(!props.locked){clearDraft();confirmOpen.value=false}})
watch(()=>props.selections.length,count=>{if(!count&&props.busy)confirmOpen.value=false})
watch(()=>user.value?.id,(next,previous)=>{if(previous&&next!==previous)confirmOpen.value=false},{flush:'sync'})
function clearDraft(){pool.value=[];pairLeft.value=[];pairRight.value=[];pairMode.value='numbers'}
function setCategory(id:string){if(!props.locked){expandedCategory.value=active.value===id&&expandedCategory.value===id?'':id;active.value=id;chipsOpen.value=false}}
function isChildActive(item:SubmenuItem){return item.type==='position'?(position.value||positions.value[0])===item.id:template.value?.id===item.id}
function selectChild(item:SubmenuItem){if(props.locked)return;if(item.type==='position')position.value=item.id;else templateID.value=item.id;if(content.value)content.value.scrollTop=0}
const displayOdds=(market:Market)=>{const quote=quotes.value[quoteID(market)];return quote?.enabled?marketOddsText(market,selectionOdds(market,quote,context.value),quote.tier_odds_milli,true):'暂停'}
function isNumber(market:Market){return /^\d{1,2}$/.test(market.label)&&!usesZodiac(market)&&!market.kind.includes('tail')}
function numberColor(number:number){return context.value?.numbers.find(item=>item.number===number)?.color??'green'}
function displayNumbers(market:Market){
 const value=Number(market.pick)
 if(['lhc_zodiac','lhc_zodiac_count'].includes(market.kind))return context.value?.zodiacs.find(item=>item.value===value||item.label===market.label)?.numbers??[]
 if(market.kind.includes('tail')||market.ui_group==='zodiac_tail'&&market.group.includes('尾'))return context.value?.tails.find(item=>item.value===value||item.label===market.label)?.numbers??[]
 return []
}
function disabledMarket(market:Market){return props.locked||!quotes.value[quoteID(market)]?.enabled||!validTierOdds(market,quotes.value[quoteID(market)]?.tier_odds_milli)||usesZodiac(market)&&!contextReady.value}
function togglePool(value:number){
 if(props.locked||!draftAvailable.value||!schema.value)return
 if(pool.value.includes(value)){pool.value=pool.value.filter(item=>item!==value);return}
 const max=schema.value.mode==='set'?schema.value.pick_count:schema.value.max_picks
 if(pool.value.length>=max){emit('notify',`当前玩法最多选择${max}项`);return}
 pool.value=canonicalValues([...pool.value,value])
}
function setPairMode(mode:'numbers'|'zodiac'|'tail'){if(props.locked)return;pairMode.value=mode;pool.value=[];pairLeft.value=[];pairRight.value=[]}
function togglePair(side:'left'|'right',value:number){if(props.locked||!draftAvailable.value)return;const selected=side==='left'?pairLeft:pairRight;selected.value=selected.value.includes(value)?selected.value.filter(item=>item!==value):canonicalValues([...selected.value,value])}
function addDraft(){if(props.locked||!draftAvailable.value||draftLimitExceeded.value||!additions.value.length)return;emit('add',additions.value);pool.value=[];pairLeft.value=[];pairRight.value=[]}
function openConfirmation(event:MouseEvent){if(props.busy)return;if(!props.selections.length){emit('notify','请先选择投注项并加入投注单');return}returnFocus=event.currentTarget as HTMLElement;chipsOpen.value=false;confirmOpen.value=true}
function closeConfirmation(){if(!props.busy)confirmOpen.value=false}
function submitConfirmation(){if(!user.value)confirmOpen.value=false;emit('submit')}
function clear(){if(props.locked)return;clearDraft();emit('clear');emit('amount','')}
function restoreOverflow(){if(priorOverflow!==null){document.body.style.overflow=priorOverflow;priorOverflow=null}}
watch(confirmOpen,async open=>{if(open){priorOverflow=document.body.style.overflow;document.body.style.overflow='hidden';await nextTick();dialog.value?.focus()}else{restoreOverflow();if(returnFocus?.isConnected)returnFocus.focus()}})
function trap(event:KeyboardEvent){if(event.key!=='Tab')return;const nodes=Array.from(dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled)')??[]).filter(node=>node.getClientRects().length);if(!nodes.length){event.preventDefault();return}const first=nodes[0]!,last=nodes[nodes.length-1]!,outside=!dialog.value?.contains(document.activeElement)||document.activeElement===dialog.value;if(event.shiftKey&&(outside||document.activeElement===first)){event.preventDefault();last.focus()}else if(!event.shiftKey&&(outside||document.activeElement===last)){event.preventDefault();first.focus()}}
onUnmounted(restoreOverflow)
</script>

<template>
 <div class="marksix-board" :class="{'marksix-zodiac-view':active==='zodiac_tail'}">
  <div class="marksix-betting-layout">
   <nav class="marksix-play-menu" aria-label="六合彩玩法分类">
    <div v-for="item in menuSections" :key="item.id" class="marksix-menu-section">
     <button :disabled="locked" :class="{active:active===item.id}" :aria-pressed="active===item.id" :aria-expanded="item.children.length?active===item.id&&expandedCategory===item.id:undefined" :aria-controls="item.children.length?`marksix-submenu-${item.id}`:undefined" @click="setCategory(item.id)"><span>{{item.label}}</span><ChevronRight v-if="item.children.length" :size="12" :class="{expanded:active===item.id&&expandedCategory===item.id}"/></button>
     <div v-if="item.children.length&&active===item.id&&expandedCategory===item.id" :id="`marksix-submenu-${item.id}`" class="marksix-left-submenu" role="group" :aria-label="`${item.label}小分类`"><button v-for="child in item.children" :key="child.id" :disabled="locked" :class="{active:isChildActive(child)}" :aria-pressed="isChildActive(child)" @click="selectChild(child)">{{child.label}}</button></div>
    </div>
   </nav>
   <div ref="content" class="marksix-play-content">
    <div v-if="active!=='zodiac_tail'" class="marksix-board-note"><span>可多选 · 每注独立结算</span><button :disabled="locked" aria-label="刷新六合彩赔率" @click="emit('refresh')"><RefreshCw :size="14"/></button></div>
    <p v-if="!contextReady" class="marksix-context-notice" role="status">生肖资料同步中，暂不可选择生肖玩法。</p>
    <template v-if="templates.length">
     <template v-if="template&&schema">
      <header class="marksix-combination-title"><h3>{{template.label}}</h3><span>赔率 {{displayOdds(template)}}</span></header>
      <p v-if="conditionalOddsHint(template,quotes[template.id])" class="marksix-play-rule">普通组合 {{displayOdds(template)}}倍；{{conditionalOddsHint(template,quotes[template.id])}}。复式按每一组实际组合分别取价，投注单显示锁定赔率。</p>
      <p class="marksix-play-rule">{{template.rule}}</p>
      <div v-if="pairAllowed" class="marksix-pair-modes" aria-label="连码选码方式"><button :disabled="locked" :class="{active:pairMode==='numbers'}" @click="setPairMode('numbers')">号码复式</button><button :disabled="locked||!contextReady" :class="{active:pairMode==='zodiac'}" @click="setPairMode('zodiac')">生肖对碰</button><button :disabled="locked" :class="{active:pairMode==='tail'}" @click="setPairMode('tail')">尾数对碰</button></div>
      <div v-if="pairAllowed&&pairMode!=='numbers'" class="marksix-pair-picker"><section v-for="side in (['left','right'] as const)" :key="side"><h4>{{side==='left'?'第一组':'第二组'}}</h4><div><button v-for="option in pairOptions" :key="option.value" :disabled="locked||!draftAvailable" :class="{selected:(side==='left'?pairLeft:pairRight).includes(option.value)}" :aria-pressed="(side==='left'?pairLeft:pairRight).includes(option.value)" @click="togglePair(side,option.value)">{{option.label}}</button></div></section><p>两组号码交叉配对，相同号码不成对，相同组合只计一注。</p></div>
      <div v-else class="marksix-pool" :class="{numbers:schema.type==='number'}" aria-label="组合选码">
       <button v-for="option in poolOptions" :key="option.value" :disabled="locked||!draftAvailable" :class="{selected:pool.includes(option.value)}" :aria-pressed="pool.includes(option.value)" :aria-label="`${template.label}选择${option.label}`" @click="togglePool(option.value)"><MarkSixBall v-if="schema.type==='number'" :number="option.value" :color="numberColor(option.value)"/><template v-else><strong>{{option.label}}</strong><span class="marksix-row-balls"><MarkSixBall v-for="number in option.numbers" :key="number" :number="number" :color="numberColor(number)"/></span></template><Check v-if="pool.includes(option.value)" :size="12"/></button>
      </div>
      <section class="marksix-draft-summary" aria-label="组合投注预览"><p>{{pairMode==='numbers'?`已选 ${pool.length} 项`:`已选 ${pairLeft.length} / ${pairRight.length} 项`}} · {{draftCount>500?'超过500':draftCount}} 注</p><strong>每注 {{money(amountCost)}} 元 · {{draftCount>500?'请减少选项':`共 ${money(draftCount*amountCost)} 元`}}</strong><small :class="{invalid:draftLimitExceeded}">{{draftNotice}}</small><button :disabled="locked||!draftAvailable||draftLimitExceeded||!additions.length" @click="addDraft">加入投注单{{additions.length&&!draftLimitExceeded?`（${additions.length}注）`:''}}</button></section>
     </template>
    </template>
    <section v-for="group in fixedGroups" :key="group.name" class="marksix-fixed-group"><h3 v-if="!(active==='zodiac_tail'&&group.name==='一肖')">{{group.name==='正特尾数'?'尾数':group.name}}</h3><div v-for="(rows,rowIndex) in group.rows" :key="rowIndex" class="marksix-fixed-options" :class="{numbers:rows.every(isNumber),rows:rows.some(market=>displayNumbers(market).length)}"><button v-for="market in rows" :key="market.id" :disabled="disabledMarket(market)" :class="{selected:selectedIDs.has(market.id)}" :aria-pressed="selectedIDs.has(market.id)" :aria-label="`${market.group}${market.label}，赔率${displayOdds(market)}`" @click="emit('select',market)"><MarkSixBall v-if="isNumber(market)" :number="Number(market.pick)" :color="numberColor(Number(market.pick))"/><strong v-else>{{market.label}}</strong><span v-if="displayNumbers(market).length" class="marksix-row-balls"><MarkSixBall v-for="number in displayNumbers(market)" :key="number" :number="number" :color="numberColor(number)"/></span><span class="marksix-option-odds">{{displayOdds(market)}}</span><Check v-if="selectedIDs.has(market.id)" :size="12"/></button></div></section>
    <p v-if="!categoryMarkets.length" class="marksix-empty">当前分类暂无可用玩法</p>
   </div>
  </div>
  <div v-if="chipsOpen" class="marksix-chip-panel"><button class="marksix-close-chips" @click="chipsOpen=false"><X :size="15"/>关闭筹码</button><StakeAmount :model-value="amount" label="每注金额" :min="min" :max="max" :disabled="locked" @update:model-value="emit('amount',$event)"/></div>
  <footer class="marksix-bet-dock"><div class="marksix-dock-amount"><button :disabled="busy" @click="openConfirmation">已选中{{selections.length}}注</button><input :value="amount" :disabled="locked" inputmode="decimal" aria-label="六合彩每注金额（元）" placeholder="请输入金额" @input="emit('amount',($event.target as HTMLInputElement).value)"/></div><button class="marksix-place" :disabled="busy||!selections.length||(blocked&&!retrying)" @click="openConfirmation">{{busy?'提交中':retrying?'重试确认':'下注'}}</button><button class="marksix-chips" :disabled="locked" :aria-expanded="chipsOpen" @click="chipsOpen=!chipsOpen">筹码</button><button class="marksix-reset" :disabled="locked" @click="clear">重置</button><p v-if="notice" class="marksix-dock-notice" role="status">{{notice}}<button v-if="canConfirmQuotes" :disabled="locked" @click="emit('confirm-quotes')">确认最新赔率</button></p></footer>
  <Teleport to="body"><div v-if="confirmOpen" class="modal-overlay marksix-confirm-overlay" @click.self="closeConfirmation" @keydown.esc.stop="closeConfirmation" @keydown="trap"><section ref="dialog" class="marksix-confirm" role="dialog" aria-modal="true" aria-labelledby="marksix-confirm-title" tabindex="-1"><header><div><h2 id="marksix-confirm-title">确认投注</h2><p>第 {{formatIssue(issue)}} 期 · {{selections.length}} 注分别结算</p></div><button :disabled="busy" aria-label="关闭六合彩投注确认" @click="closeConfirmation"><X :size="20"/></button></header><div class="marksix-confirm-body"><StakeAmount :model-value="amount" label="每注金额" :min="min" :max="max" :disabled="locked" @update:model-value="emit('amount',$event)"/><div class="marksix-confirm-selections" aria-label="已选六合彩投注"><article v-for="selection in selections" :key="selection.market.id"><div><strong>{{selection.market.group}} · {{selection.market.label}}</strong><small>赔率 {{marketOddsText(selection.market,selection.odds,selection.tierOdds)}} · 每注 {{money(amountCost)}} 元</small><small v-if="selection.market.kind==='lhc_combined_zodiac'">只看第7个特码，前6个正码不参与；所选生肖合为1注，开49退本。</small><small v-if="selection.market.payout_tiers?.length">同一组选数只计1注、扣款1次，按中奖档位派彩，不叠加两档。</small><small v-if="(selection.market.max_payout_multiplier??1)>1">按前6个正码中该生肖出现次数派彩，最多{{selection.market.max_payout_multiplier}}次。</small></div><button :disabled="locked" :aria-label="`移除${selection.market.label}`" @click="emit('remove',selection.market.id)"><X :size="15"/></button></article></div><p v-if="notice" class="marksix-confirm-notice" role="status">{{notice}}<button v-if="canConfirmQuotes" :disabled="locked" @click="emit('confirm-quotes')">确认最新赔率</button></p></div><footer><div><small>{{selections.length}} 注 × {{money(amountCost)}} 元</small><strong>合计 {{money(total)}} 元</strong></div><button :disabled="submitDisabled" @click="submitConfirmation">{{busy?'提交中…':retrying?'重试确认':!user?'登录后投注':'确认投注'}}</button></footer></section></div></Teleport>
 </div>
</template>

<style scoped>
.marksix-board{display:flex;flex-direction:column;flex:1;min-height:0;overflow:hidden;position:relative;min-width:0;background:#fff;color:#333}.marksix-betting-layout{display:grid;grid-template-columns:120px minmax(0,1fr);flex:1;min-height:0;overflow:hidden}.marksix-play-menu{overflow-y:auto;overscroll-behavior:contain;background:#f8f8f8;border-right:1px solid #dedee2;min-width:0}.marksix-play-menu button{display:flex;justify-content:space-between;align-items:center;gap:2px;width:100%;min-height:47px;padding:11px 13px;border-bottom:1px solid #e4e4e8;color:#414149;font-size:13px;text-align:left}.marksix-play-menu button.active{background:#ece7fb;color:#7950c8;box-shadow:inset 3px 0 #9371df}.marksix-play-content{min-width:0;min-height:0;overflow-y:auto;overscroll-behavior:contain;padding-bottom:15px}.marksix-board-note{display:flex;align-items:center;justify-content:space-between;min-height:30px;padding:4px 12px;border-bottom:1px solid #e9e9ec;color:#a49aa6;font-size:10px}.marksix-board-note button{width:25px;height:23px;display:grid;place-items:center;color:#9987ae}.marksix-context-notice,.marksix-play-rule{padding:10px 12px;font-size:11px;line-height:1.7;color:#94838c;background:#fffaf3;margin:0;overflow-wrap:anywhere}.marksix-subnav{display:flex;flex-wrap:wrap;gap:5px;padding:10px;border-bottom:1px solid #e7e4eb;background:#faf9fc}.marksix-subnav button{padding:6px 9px;border:1px solid #e4ddec;background:#fff;border-radius:4px;font-size:12px;color:#796d84}.marksix-subnav button.active{background:#8c6bd5;border-color:#8c6bd5;color:#fff}.marksix-fixed-group h3,.marksix-combination-title{display:flex;align-items:center;justify-content:space-between;gap:10px;margin:0;padding:9px 12px;background:#f7f7f8;color:#ae7376;border-bottom:1px solid #e4e4e8;font-size:12px;font-weight:400}.marksix-combination-title h3{font-size:13px;margin:0;font-weight:500}.marksix-combination-title{flex-wrap:wrap}.marksix-combination-title span{font-size:11px;white-space:normal;line-height:1.6}.marksix-fixed-options{display:grid;grid-template-columns:repeat(2,minmax(0,1fr))}.marksix-fixed-options.numbers{grid-template-columns:repeat(6,minmax(0,1fr))}.marksix-fixed-options.rows{grid-template-columns:1fr}.marksix-fixed-options>button{position:relative;display:flex;align-items:center;justify-content:center;gap:18px;min-width:0;min-height:47px;padding:9px 16px 9px 10px;border-bottom:1px solid #e5e5e8;border-right:1px solid #e5e5e8;background:#fff;font-size:14px}.marksix-fixed-options strong{font-size:15px;font-weight:500;overflow-wrap:anywhere}.marksix-option-odds{font-size:12px;color:#7d747e;font-variant-numeric:tabular-nums;white-space:nowrap}.marksix-fixed-options.rows>button{justify-content:flex-start;gap:12px}.marksix-fixed-options.rows strong{width:46px;flex-shrink:0}.marksix-fixed-options.rows .marksix-option-odds{margin-left:auto}.marksix-row-balls{display:flex;gap:4px;align-items:center;flex-wrap:wrap;min-width:0}.marksix-bet-ball{--ball:#32804b;display:inline-flex;align-items:center;justify-content:center;width:29px;height:29px;border-radius:50%;flex-shrink:0;background:var(--ball);box-shadow:inset 0 0 0 2px #ffffff66,0 1px 2px #0002;border:2px dashed #ffffffd9;color:#fff;font-size:12px;font-weight:700;font-style:normal;font-variant-numeric:tabular-nums;line-height:1}.marksix-bet-ball.red{--ball:#c9363e}.marksix-bet-ball.blue{--ball:#2662b4}.marksix-fixed-options.numbers>button{flex-direction:column;gap:5px;padding:9px 3px}.marksix-fixed-options button>svg,.marksix-pool button>svg{position:absolute;right:3px;top:3px;color:#8863c0}.marksix-board button.selected{background:#f1e9fd;box-shadow:inset 0 0 0 1px #b395d8}.marksix-board button.selected strong{color:#8054bd}.marksix-pool{display:grid;grid-template-columns:1fr}.marksix-pool.numbers{grid-template-columns:repeat(6,minmax(0,1fr))}.marksix-pool>button{position:relative;display:flex;align-items:center;gap:12px;min-width:0;min-height:46px;padding:8px 14px;border-right:1px solid #e5e5e8;border-bottom:1px solid #e5e5e8}.marksix-pool>button strong{min-width:37px;font-size:14px;font-weight:500}.marksix-pool.numbers>button{justify-content:center;padding:9px 3px}.marksix-pair-modes{display:flex;gap:4px;padding:9px 10px;background:#f8f6fa}.marksix-pair-modes button{padding:6px 8px;border:1px solid #e3d9ed;background:#fff;border-radius:4px;font-size:11px;color:#89729e}.marksix-pair-modes button.active{background:#efe7fa;border-color:#b79bd5;color:#7d52b4}.marksix-pair-picker{padding:11px}.marksix-pair-picker section+section{margin-top:14px}.marksix-pair-picker h4{margin:0 0 7px;font-size:12px;font-weight:500;color:#8b738b}.marksix-pair-picker section>div{display:grid;grid-template-columns:repeat(6,minmax(0,1fr));gap:5px}.marksix-pair-picker section button{min-height:35px;padding:5px 2px;border:1px solid #ded7e5;border-radius:4px;font-size:12px}.marksix-pair-picker>p{margin:12px 0 0;font-size:10px;line-height:1.6;color:#a198a5}.marksix-draft-summary{padding:14px;border-bottom:1px solid #e5e0eb;background:#faf8fc}.marksix-draft-summary p{margin:0 0 7px;font-size:12px}.marksix-draft-summary strong{display:block;font-size:13px;font-weight:500;color:#7f589e}.marksix-draft-summary small{display:block;margin:7px 0;font-size:10px;line-height:1.5;color:#a08d9e}.marksix-draft-summary small.invalid{color:#bf6471}.marksix-draft-summary button{width:100%;margin-top:5px;min-height:36px;border-radius:4px;background:#8a65d2;color:#fff;font-size:12px}.marksix-empty{padding:30px 10px;color:#a093aa;font-size:12px;text-align:center}.marksix-bet-dock{flex-shrink:0;display:flex;flex-wrap:wrap;align-items:center;gap:8px;padding:10px 14px;background:#666;border-top:1px solid #595959;color:#fff}.marksix-dock-amount{flex:1;min-width:0}.marksix-dock-amount>button{display:flex;gap:10px;align-items:baseline;width:100%;padding:0 0 7px;font-size:13px;font-weight:600}.marksix-dock-amount small{font-size:10px;font-weight:400}.marksix-dock-amount input{width:100%;min-width:0;height:34px;border:1px solid #ddd;border-radius:4px;background:#fff;color:#5a5261;padding:6px 9px;font-size:13px;outline:none}.marksix-dock-amount input:focus{border-color:#b29ae5;box-shadow:0 0 0 2px #b29ae550}.marksix-place,.marksix-chips,.marksix-reset{min-height:57px;min-width:64px;padding:10px;border-radius:4px;font-size:13px;font-weight:600}.marksix-place{background:#95b86d}.marksix-chips{background:#ce6b4f}.marksix-reset{background:#557fba}.marksix-dock-notice{flex-basis:100%;margin:0;font-size:10px;line-height:1.6;color:#ffecce}.marksix-dock-notice button{margin-left:8px;text-decoration:underline;color:#fff}.marksix-chip-panel{flex-shrink:0;max-height:40%;overflow-y:auto;padding:12px;background:#fff;border-top:1px solid #e5e0ed}.marksix-close-chips{display:flex;gap:5px;align-items:center;margin-left:auto;font-size:11px;color:#a08cad}.marksix-chip-panel :deep(.stake-control){margin:5px 0;padding:12px}.marksix-board button:disabled,.marksix-board input:disabled,.marksix-confirm button:disabled,.marksix-confirm input:disabled{opacity:.5;cursor:not-allowed}
.marksix-confirm-overlay{z-index:65;padding:15px}.marksix-confirm{display:flex;flex-direction:column;width:610px;max-width:100%;max-height:calc(100dvh - 30px);border-radius:12px;background:#fff;color:#62556f;overflow:hidden;outline:none}.marksix-confirm>header{display:flex;align-items:flex-start;justify-content:space-between;gap:10px;padding:18px;border-bottom:1px solid #e8e1ed}.marksix-confirm h2{margin:0;font-size:19px;color:#514159}.marksix-confirm header p{margin:7px 0 0;font-size:11px;color:#a08ba8}.marksix-confirm header button{width:28px;height:28px;display:grid;place-items:center;color:#9c8da8}.marksix-confirm-body{min-height:0;overflow:auto;padding:15px 18px}.marksix-confirm-body :deep(.stake-control){margin:0 0 13px;padding:0 0 13px;border:0;border-bottom:1px solid #eee8f3;border-radius:0}.marksix-confirm-selections{display:grid;gap:7px}.marksix-confirm-selections article{display:flex;align-items:center;justify-content:space-between;gap:7px;padding:10px;border:1px solid #e9e1f0;border-radius:5px;background:#fbf8fe}.marksix-confirm-selections article>div{min-width:0}.marksix-confirm-selections strong{display:block;font-size:12px;line-height:1.6;overflow-wrap:anywhere}.marksix-confirm-selections small{display:block;font-size:10px;color:#9c8ba8;margin-top:4px}.marksix-confirm-selections button{flex-shrink:0;width:25px;height:26px;display:grid;place-items:center;color:#a28caf}.marksix-confirm-notice{padding:10px;background:#fff8ef;border-radius:5px;color:#b17e77;font-size:11px;line-height:1.7}.marksix-confirm-notice button{margin-left:5px;text-decoration:underline;color:#8c62b4}.marksix-confirm>footer{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:13px 18px;border-top:1px solid #ebe4f1}.marksix-confirm footer small{display:block;font-size:11px;color:#a08aa6}.marksix-confirm footer strong{display:block;margin-top:4px;font-size:16px;color:#9567b1;font-weight:500}.marksix-confirm footer>button{min-height:40px;padding:10px 24px;border-radius:5px;background:#8960d0;color:white;font-size:13px}
@media(max-width:760px){.marksix-betting-layout{grid-template-columns:76px minmax(0,1fr);min-height:0}.marksix-play-menu button{min-height:45px;padding:11px 7px;font-size:11px}.marksix-play-menu button>svg{width:10px}.marksix-board-note{padding:3px 8px;font-size:9px}.marksix-fixed-group h3{padding:8px;font-size:11px}.marksix-fixed-options.numbers,.marksix-pool.numbers{grid-template-columns:repeat(6,minmax(0,1fr))}.marksix-fixed-options>button{min-height:44px;gap:9px;padding:9px 10px 9px 7px}.marksix-fixed-options strong{font-size:13px}.marksix-option-odds{font-size:10px}.marksix-fixed-options.rows>button{gap:6px}.marksix-fixed-options.rows strong{width:28px;font-size:12px}.marksix-row-balls{gap:3px}.marksix-bet-ball{width:25px;height:25px;font-size:10px}.marksix-pool>button{gap:7px;padding:8px}.marksix-pool>button strong{min-width:28px;font-size:12px}.marksix-subnav{padding:7px;gap:4px}.marksix-subnav button{font-size:10px;padding:6px}.marksix-combination-title{padding:8px}.marksix-combination-title h3{font-size:12px}.marksix-combination-title span{font-size:10px}.marksix-play-rule,.marksix-context-notice{padding:8px;font-size:10px}.marksix-pair-modes{padding:7px 6px;gap:3px}.marksix-pair-modes button{font-size:10px;padding:6px}.marksix-pair-picker{padding:9px 7px}.marksix-pair-picker section>div{grid-template-columns:repeat(4,minmax(0,1fr))}.marksix-draft-summary{padding:11px 9px}.marksix-draft-summary strong{font-size:11px}.marksix-bet-dock{z-index:5;gap:6px;padding:8px 8px calc(8px + env(safe-area-inset-bottom,0px))}.marksix-dock-amount>button{gap:4px;flex-wrap:wrap;font-size:12px;padding-bottom:5px}.marksix-dock-amount small{font-size:9px}.marksix-place,.marksix-chips,.marksix-reset{min-width:44px;padding:9px 7px;font-size:12px;min-height:58px}.marksix-dock-amount input{height:33px;font-size:12px;padding:6px}.marksix-dock-notice{font-size:9px}.marksix-confirm-overlay{padding:10px}.marksix-confirm{max-height:calc(100dvh - 20px)}.marksix-confirm>header{padding:15px}.marksix-confirm-body{padding:12px 14px}.marksix-confirm footer>button{padding:10px 17px}.marksix-confirm-body :deep(.stake-shortcuts){grid-template-columns:repeat(3,minmax(0,1fr))}}
@media(max-width:360px){.marksix-betting-layout{grid-template-columns:68px minmax(0,1fr)}.marksix-play-menu button{padding:11px 5px;font-size:10px}.marksix-row-balls{gap:2px}.marksix-bet-ball{width:23px;height:23px;font-size:9px}.marksix-fixed-options.rows>button{gap:4px;padding-left:5px;padding-right:8px}.marksix-fixed-options.rows strong{width:22px}.marksix-option-odds{font-size:9px}.marksix-pool>button{gap:5px}.marksix-pair-modes button{font-size:9px;padding:6px 4px}.marksix-bet-dock{gap:5px;padding-left:6px;padding-right:6px}.marksix-place,.marksix-chips,.marksix-reset{min-width:40px;padding-left:6px;padding-right:6px}}
</style>

<style scoped>
/* Dimensions follow the 587px reference; quote and order values stay unchanged. */
.marksix-board{--lhc-unit:min(1px,calc(100vw / 587));--marksix-ball-size:calc(34 * var(--lhc-unit));--marksix-ball-font:calc(18 * var(--lhc-unit));font-family:Arial,'Microsoft YaHei','PingFang SC',sans-serif;color:#111}
.marksix-betting-layout{grid-template-columns:min(23.51%,138px) minmax(0,1fr)}
.marksix-play-menu{background:#f2f2f2;border-right:1px solid #d4d4d4;scrollbar-width:none}
.marksix-play-menu::-webkit-scrollbar,.marksix-play-content::-webkit-scrollbar{display:none}
.marksix-play-menu button{position:relative;justify-content:center;min-height:0;height:calc(56 * var(--lhc-unit));padding:0 calc(18 * var(--lhc-unit));font-size:calc(20 * var(--lhc-unit));color:#222;background:#fff;border-bottom:1px solid #eee;text-align:center;white-space:nowrap}
.marksix-play-menu button>svg{position:absolute;right:calc(10 * var(--lhc-unit));width:calc(18 * var(--lhc-unit));height:calc(18 * var(--lhc-unit));color:#929292}
.marksix-play-menu button.active{background:#fafafa;color:#111;box-shadow:none}
.marksix-play-menu button:focus-visible{outline-offset:-3px}
.marksix-menu-section>button.active{background:linear-gradient(110deg,#6656df,#8054dc);color:#fff;font-weight:600}
.marksix-menu-section>button.active>svg{color:#fff}
.marksix-menu-section>button>svg.expanded{transform:rotate(90deg)}
.marksix-play-menu .marksix-left-submenu button{height:auto;min-height:calc(48 * var(--lhc-unit));padding:calc(9 * var(--lhc-unit)) calc(7 * var(--lhc-unit));background:#f8f6fc;color:#645e70;border-bottom:1px solid #eeebf4;font-size:max(10px,calc(18 * var(--lhc-unit)));line-height:1.35;white-space:normal;overflow-wrap:anywhere}
.marksix-play-menu .marksix-left-submenu button.active{background:#eae2fa;color:#7047b7;box-shadow:inset 3px 0 #9671d5;font-weight:600}
.marksix-play-content{padding-bottom:0;scrollbar-width:none}
.marksix-fixed-group h3{justify-content:center;min-height:calc(32 * var(--lhc-unit));padding:calc(5 * var(--lhc-unit));font-size:calc(19 * var(--lhc-unit));line-height:1.15;background:#f1f1f1;color:#a56b6b;border-bottom:1px solid #ececec}
.marksix-fixed-options>button{min-height:calc(62 * var(--lhc-unit));border-bottom:1px solid #e5e5e5;border-right:1px solid #ededed;color:#111}
.marksix-fixed-options.rows>button{gap:calc(12 * var(--lhc-unit));height:calc(62 * var(--lhc-unit));padding:0 calc(13 * var(--lhc-unit)) 0 calc(16 * var(--lhc-unit));justify-content:flex-start}
.marksix-fixed-options.rows strong{width:calc(37 * var(--lhc-unit));font-size:calc(22 * var(--lhc-unit));font-weight:700;line-height:1.2;color:#111}
.marksix-option-odds{font-size:calc(20 * var(--lhc-unit));font-weight:400;line-height:1;color:#161616}
.marksix-row-balls{flex-wrap:nowrap;gap:calc(4 * var(--lhc-unit));overflow:visible}
.marksix-fixed-options.numbers{grid-template-columns:repeat(6,minmax(0,1fr))}
.marksix-fixed-options.numbers>button{padding:calc(10 * var(--lhc-unit)) 0;gap:calc(6 * var(--lhc-unit))}
.marksix-fixed-options.numbers .marksix-option-odds{font-size:calc(16 * var(--lhc-unit))}
.marksix-fixed-options:not(.rows):not(.numbers) strong{font-size:calc(22 * var(--lhc-unit))}
.marksix-board button.selected{background:#f2eaff;box-shadow:inset 0 0 0 1px #bda4eb}
.marksix-board button.selected strong{color:#6f44bd}
.marksix-pool{--marksix-ball-size:calc(34 * var(--lhc-unit));--marksix-ball-font:calc(18 * var(--lhc-unit))}
.marksix-pool>button{min-height:calc(62 * var(--lhc-unit));gap:calc(12 * var(--lhc-unit));padding:calc(9 * var(--lhc-unit)) calc(16 * var(--lhc-unit))}
.marksix-pool>button strong{min-width:calc(37 * var(--lhc-unit));font-size:calc(22 * var(--lhc-unit));font-weight:600;color:#111}
.marksix-pool.numbers{grid-template-columns:repeat(6,minmax(0,1fr))}
.marksix-pool.numbers>button{padding:calc(9 * var(--lhc-unit)) 0}
.marksix-bet-dock{display:grid;grid-template-columns:minmax(0,1fr) repeat(3,calc(83 * var(--lhc-unit)));align-items:stretch;gap:calc(14 * var(--lhc-unit));padding:calc(14 * var(--lhc-unit)) calc(14 * var(--lhc-unit)) calc(13 * var(--lhc-unit) + env(safe-area-inset-bottom,0px));border-top:0;background:#666;min-height:calc(110 * var(--lhc-unit))}
.marksix-dock-amount{display:flex;flex-direction:column;justify-content:space-between;gap:calc(9 * var(--lhc-unit))}
.marksix-dock-amount>button{display:block;font-size:calc(25 * var(--lhc-unit));line-height:1.25;font-weight:700;padding:0;text-align:left;white-space:nowrap}
.marksix-dock-amount input{height:calc(42 * var(--lhc-unit));border:0;border-radius:calc(6 * var(--lhc-unit));padding:0 calc(9 * var(--lhc-unit));font-size:calc(18 * var(--lhc-unit));color:#222}
.marksix-dock-amount input::placeholder{color:#bbb}
.marksix-place,.marksix-chips,.marksix-reset{min-width:0;min-height:calc(83 * var(--lhc-unit));padding:0 calc(4 * var(--lhc-unit));border-radius:calc(6 * var(--lhc-unit));font-size:calc(20 * var(--lhc-unit));font-weight:700;line-height:1.25;color:#fff}
.marksix-place{background:#9fbd7c}.marksix-chips{background:#c66e51}.marksix-reset{background:#557cbd}
.marksix-board .marksix-place:disabled{opacity:1;filter:saturate(.9)}
.marksix-dock-notice{grid-column:1 / -1;font-size:max(10px,calc(16 * var(--lhc-unit)))}
@media(min-width:761px){.marksix-betting-layout{grid-template-columns:138px minmax(0,1fr)}.marksix-fixed-options.rows>button{padding-right:24px}.marksix-bet-dock{grid-template-columns:minmax(0,1fr) repeat(3,83px)}.marksix-dock-amount{max-width:420px}}
</style>
