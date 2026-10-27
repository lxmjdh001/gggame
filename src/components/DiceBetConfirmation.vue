<script setup lang="ts">
import {nextTick,onMounted,onUnmounted,ref} from 'vue'
import {X} from 'lucide-vue-next'
import StakeAmount from './StakeAmount.vue'
import {money} from '../api/account'
import {oddsText,type Market} from '../api/markets'
import {parseStake} from '../lib/stake'
import {formatIssue} from '../lib/issueDisplay'
const props=defineProps<{selections:{market:Market;odds:number}[];issue:string;amount:string;min:number;max:number;total:number;locked:boolean;busy:boolean;retrying:boolean;loggedIn:boolean;submitDisabled:boolean;notice:string;canConfirmQuotes:boolean}>()
const emit=defineEmits<{close:[];submit:[];remove:[id:string];amount:[value:string];'confirm-quotes':[]}>()
const dialog=ref<HTMLElement|null>(null)
let previousOverflow:string|undefined,opener:HTMLElement|null=null,mounted=true
onMounted(async()=>{opener=document.activeElement instanceof HTMLElement?document.activeElement:null;previousOverflow=document.body.style.overflow;document.body.style.overflow='hidden';await nextTick();if(mounted)dialog.value?.focus()})
onUnmounted(()=>{mounted=false;if(previousOverflow!==undefined)document.body.style.overflow=previousOverflow;if(opener?.isConnected)opener.focus()})
function close(){if(!props.busy)emit('close')}
function trap(event:KeyboardEvent){
 if(event.key!=='Tab')return
 const nodes=Array.from(dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled)')??[]).filter(node=>node.getClientRects().length)
 if(!nodes.length){event.preventDefault();return}
 const first=nodes[0]!,last=nodes[nodes.length-1]!,outside=!dialog.value?.contains(document.activeElement)||document.activeElement===dialog.value
 if(event.shiftKey&&(outside||document.activeElement===first)){event.preventDefault();last.focus()}
 else if(!event.shiftKey&&(outside||document.activeElement===last)){event.preventDefault();first.focus()}
}
</script>
<template>
 <Teleport to="body"><div class="dice-confirm-overlay" @click.self="close" @keydown.esc.stop="close" @keydown="trap">
  <section ref="dialog" class="dice-confirm" role="dialog" aria-modal="true" aria-labelledby="dice-confirm-title" tabindex="-1">
   <header><div><h2 id="dice-confirm-title">确认下注</h2><p>第 {{formatIssue(issue)}} 期 · {{selections.length}} 注分别结算</p></div><button type="button" :disabled="busy" aria-label="关闭快三投注确认" @click="close"><X :size="20"/></button></header>
   <div class="dice-confirm-body">
    <StakeAmount :model-value="amount" label="每注金额" :min="min" :max="max" :disabled="locked" @update:model-value="emit('amount',$event)"/>
    <div class="dice-confirm-selections" aria-label="已选快三投注"><article v-for="selection in selections" :key="selection.market.id"><div><strong>{{selection.market.group}} · {{selection.market.label}}</strong><small>赔率 {{oddsText(selection.odds)}} · 每注 {{money(parseStake(amount))}} 元</small></div><button type="button" :disabled="locked" :aria-label="`移除${selection.market.group}${selection.market.label}`" @click="emit('remove',selection.market.id)"><X :size="15"/></button></article></div>
    <p v-if="notice" class="dice-confirm-notice" role="status">{{notice}}<button v-if="canConfirmQuotes" type="button" :disabled="locked" @click="emit('confirm-quotes')">确认最新赔率</button></p>
   </div>
   <footer><div><small>{{selections.length}} 注 × {{money(parseStake(amount))}} 元</small><strong>合计 {{money(total)}} 元</strong></div><button type="button" class="dice-confirm-cancel" :disabled="busy" @click="close">取消</button><button type="button" class="dice-confirm-submit" :disabled="submitDisabled" @click="emit('submit')">{{busy?'提交中…':retrying?'重试确认':loggedIn?'确定下注':'登录后投注'}}</button></footer>
  </section>
 </div></Teleport>
</template>
<style scoped>
.dice-confirm-overlay{position:fixed;inset:0;z-index:65;display:flex;align-items:center;justify-content:center;background:#21182e7a;padding:15px}.dice-confirm{display:flex;flex-direction:column;width:610px;max-width:100%;max-height:calc(100vh - 30px);max-height:calc(100dvh - 30px);border-radius:12px;background:#fff;color:#62556f;overflow:hidden;outline:none}.dice-confirm>header{display:flex;align-items:flex-start;justify-content:space-between;gap:10px;padding:18px;border-bottom:1px solid #e8e1ed}.dice-confirm h2{margin:0;font-size:19px;color:#514159}.dice-confirm header p{margin:7px 0 0;font-size:11px;color:#a08ba8}.dice-confirm header button{width:28px;height:28px;display:grid;place-items:center;color:#9c8da8}.dice-confirm-body{min-height:0;overflow:auto;padding:15px 18px;overscroll-behavior:contain;-webkit-overflow-scrolling:touch}.dice-confirm-body :deep(.stake-control){margin:0 0 13px;padding:0 0 13px;border:0;border-bottom:1px solid #eee8f3;border-radius:0}.dice-confirm-selections{display:grid;gap:7px}.dice-confirm-selections article{display:flex;align-items:center;justify-content:space-between;gap:7px;padding:10px;border:1px solid #e9e1f0;border-radius:5px;background:#fbf8fe}.dice-confirm-selections article>div{min-width:0}.dice-confirm-selections strong{display:block;font-size:12px;line-height:1.6;overflow-wrap:anywhere}.dice-confirm-selections small{display:block;font-size:10px;color:#9c8ba8;margin-top:4px}.dice-confirm-selections button{flex-shrink:0;width:25px;height:26px;display:grid;place-items:center;color:#a28caf}.dice-confirm-notice{padding:10px;background:#fff8ef;border-radius:5px;color:#b17e77;font-size:11px;line-height:1.7}.dice-confirm-notice button{margin-left:5px;text-decoration:underline;color:#8c62b4}.dice-confirm>footer{display:flex;align-items:center;justify-content:space-between;gap:10px;padding:13px 18px;border-top:1px solid #ebe4f1}.dice-confirm footer>div{flex:1;min-width:0}.dice-confirm footer small{display:block;font-size:11px;color:#a08aa6}.dice-confirm footer strong{display:block;margin-top:4px;font-size:16px;color:#9567b1;font-weight:500}.dice-confirm footer>button{min-height:40px;padding:10px 20px;border-radius:5px;font-size:13px;white-space:nowrap}.dice-confirm-submit{background:#8960d0;color:#fff}.dice-confirm-cancel{background:#f3eff8;color:#8a729c}.dice-confirm button:disabled,.dice-confirm :deep(input:disabled){opacity:.5;cursor:not-allowed}.dice-confirm button:focus-visible{outline:2px solid #b797e5;outline-offset:2px}@media(max-width:760px){.dice-confirm-overlay{padding:10px}.dice-confirm{max-height:calc(100vh - 20px);max-height:calc(100dvh - 20px)}.dice-confirm>header{padding:15px}.dice-confirm-body{padding:12px 14px}.dice-confirm footer>button{padding:10px 13px}.dice-confirm>footer{padding:12px 14px;gap:7px}}@media(max-width:360px){.dice-confirm footer strong{font-size:14px}.dice-confirm footer>button{padding:9px 11px;font-size:12px}}
</style>
