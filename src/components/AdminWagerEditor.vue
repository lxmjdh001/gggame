<script setup lang="ts">
import {ref,computed,watch,onMounted,onUnmounted,nextTick} from 'vue'
import {X,RefreshCw} from 'lucide-vue-next'
import {api,cents,money,key,type Wagering} from '../api/account'
const props=defineProps<{member:{id:string;username:string}}>()
const emit=defineEmits<{close:[];saved:[];notify:[message:string]}>()
const progress=ref<Wagering|null>(null),amount=ref(''),requiredAmount=ref(''),forceClear=ref(false),green=ref(false),note=ref(''),error=ref(''),loading=ref(false),saving=ref(false),requestKey=ref(key()),dialog=ref<HTMLElement|null>(null)
const clearing=computed(()=>{try{return !!progress.value&&cents(amount.value)===0&&(forceClear.value||progress.value.turnover_cents!==0)}catch{return false}})
const requiredInput=computed({get:()=>clearing.value?'0.00':requiredAmount.value,set:(value:string)=>{requiredAmount.value=value}})
watch(amount,()=>{try{if(cents(amount.value)!==0)forceClear.value=false}catch{forceClear.value=false}})
let active=true,previousFocus:HTMLElement|null=null
async function load(){if(loading.value||saving.value)return;loading.value=true;error.value='';try{const next=await api<Wagering>(`/admin/users/${props.member.id}/wagering`);if(active){progress.value=next;forceClear.value=false;amount.value=(next.turnover_cents/100).toFixed(2);requiredAmount.value=(next.required_cents/100).toFixed(2);green.value=next.green_channel;note.value='';requestKey.value=key()}}catch(e){if(active)error.value=(e as Error).message}finally{if(active)loading.value=false}}
function clearBoth(){if(!progress.value||loading.value||saving.value)return;amount.value='0.00';forceClear.value=true}
async function save(){
 if(!progress.value||saving.value||loading.value)return
 error.value=''
 try{
  const turnover=cents(amount.value),required=cents(requiredInput.value),reason=note.value.trim()
  if(turnover<0||required<0)throw new Error('打码量和打码流水不能为负数')
  if(reason.length<2||reason.length>500)throw new Error('请填写2–500字调整原因')
  const body={version:progress.value.version,revision:progress.value.revision,green_channel:green.value,note:reason,key:requestKey.value,
   ...(clearing.value?{turnover_cents:0,required_cents:0}:{...(turnover!==progress.value.turnover_cents?{turnover_cents:turnover}:{}),...(required!==progress.value.required_cents?{required_cents:required}:{})})}
  saving.value=true
  await api(`/admin/users/${props.member.id}/wagering`,'POST',body)
  if(active){emit('notify','打码设置已更新，已记录操作日志');emit('saved');emit('close')}
 }catch(e){if(active)error.value=(e as Error).message}finally{if(active)saving.value=false}
}
function close(){if(!saving.value)emit('close')}
function trap(e:KeyboardEvent){if(e.key!=='Tab')return;const els=dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),textarea:not(:disabled)');if(!els?.length)return;const first=els[0],last=els[els.length-1];if(e.shiftKey&&(document.activeElement===first||document.activeElement===dialog.value)){e.preventDefault();last?.focus()}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus()}}
onMounted(async()=>{previousFocus=document.activeElement as HTMLElement;await nextTick();dialog.value?.focus();void load()})
onUnmounted(()=>{active=false;previousFocus?.focus()})
</script>
<template>
 <div class="modal-overlay" @click.self="close" @keydown.esc="close" @keydown="trap">
  <section ref="dialog" class="modal wager-editor" role="dialog" aria-modal="true" aria-labelledby="wager-editor-title" tabindex="-1">
   <button class="modal-close icon-button" :disabled="saving" aria-label="关闭打码设置" @click="close"><X/></button>
   <h2 id="wager-editor-title">打码设置 · {{member.username}}</h2>
   <p v-if="error" class="form-error" role="alert">{{error}}</p>
   <button class="text-button" :disabled="loading||saving" @click="load"><RefreshCw :size="14"/>重新读取</button>
   <p v-if="loading" class="loading-line">读取打码进度…</p>
   <form v-if="progress" class="form-stack" @submit.prevent="save">
    <div class="wager-summary"><p>打码量 <strong>{{money(progress.required_cents)}} 元</strong></p><p>本轮充值 {{money(progress.deposit_cents)}} 元＋奖励 {{money(progress.reward_cents)}} 元（打码量可含人工调整）</p><p>当前打码流水 {{money(progress.turnover_cents)}} 元</p></div>
    <label>调整后打码量（元）<input v-model="requiredInput" :disabled="saving||loading||clearing" required inputmode="decimal"/></label>
    <label>调整后打码流水（元）<input v-model="amount" :disabled="saving||loading" required inputmode="decimal"/></label>
    <button type="button" class="secondary-button" :disabled="saving||loading" @click="clearBoth">清零两项</button>
    <p v-if="clearing" class="wager-clear-warning" role="status">保存后，打码流水和打码量将同时清零；此后新增充值、奖励和投注重新累计。</p>
    <label class="wager-green"><input v-model="green" type="checkbox" :disabled="saving||loading"/>绿色通道：豁免打码限制</label>
    <label>调整原因<textarea v-model="note" :disabled="saving||loading" required minlength="2" maxlength="500" rows="3"/></label>
    <p class="form-hint">打码量与打码流水可分别修改；未修改项保持原值。将打码流水设为0会同时清零打码量，“清零两项”需点击保存才生效。后续充值、奖励和投注继续累计。打码流水≥打码量即可申请提现，相等也允许；绿色通道不免除提现手续费。</p>
    <button class="primary-button" :disabled="saving||loading">{{saving?'保存中…':'保存并记录操作'}}</button>
   </form>
  </section>
 </div>
</template>
<style scoped>.wager-editor{text-align:left;max-height:90vh;max-height:90dvh;overflow:auto}.wager-editor h2{font-size:21px;padding-right:20px;overflow-wrap:anywhere}.wager-editor .text-button{display:flex;gap:6px;margin:16px 0}.wager-summary{background:#faf7ff;padding:12px;font-size:13px;line-height:1.8}.wager-summary p{margin:4px 0}.wager-green{display:flex!important;align-items:center;gap:8px}.wager-green input{width:17px!important;height:17px}.wager-editor .form-hint{line-height:1.8}.wager-clear-warning{margin:0;padding:10px;border:1px solid #ead8aa;border-radius:5px;background:#fff8e8;color:#936b31;font-size:12px;line-height:1.7}</style>
