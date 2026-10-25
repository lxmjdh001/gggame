<script setup lang="ts">
import {nextTick,onMounted,onUnmounted,ref,watch} from 'vue'
import {X} from 'lucide-vue-next'
import {api,key,user} from '../api/account'
const props=defineProps<{member:{id:string;username:string}}>()
const emit=defineEmits<{close:[];saved:[];notify:[message:string]}>()
interface InviterState{username:string;inviter_id:string;inviter_username:string;new_inviter_id:string;new_inviter_username:string;descendants:number;revision:string}
const state=ref<InviterState|null>(null),plan=ref<InviterState|null>(null),parentName=ref(''),detach=ref(false),note=ref(''),loading=ref(false),saving=ref(false),error=ref(''),requestKey=ref(key()),dialog=ref<HTMLElement|null>(null)
let generation=0,previousFocus:HTMLElement|null=null
watch([parentName,detach],()=>{generation++;loading.value=false;plan.value=null;requestKey.value=key()})
async function load(){
 const ticket=++generation,owner=user.value?.id;state.value=null;plan.value=null;error.value='';loading.value=true
 try{const next=await api<InviterState>(`/admin/users/${props.member.id}/inviter`);if(ticket===generation&&user.value?.id===owner)state.value=next}
 catch(e){if(ticket===generation)error.value=e instanceof Error?e.message:'读取上级失败'}
 finally{if(ticket===generation)loading.value=false}
}
async function preview(){
 if(loading.value||saving.value||!state.value)return
 if(!detach.value&&!parentName.value.trim()){error.value='请输入完整的新上级账号';return}
 const ticket=++generation,owner=user.value?.id;loading.value=true;error.value='';plan.value=null
 try{const next=await api<InviterState>(`/admin/users/${props.member.id}/inviter/preview`,'POST',{inviter_username:detach.value?'':parentName.value.trim(),detach:detach.value});if(ticket===generation&&user.value?.id===owner){plan.value=next;requestKey.value=key()}}
 catch(e){if(ticket===generation)error.value=e instanceof Error?e.message:'上级关系预览失败'}
 finally{if(ticket===generation)loading.value=false}
}
async function save(){
 if(!plan.value||saving.value||loading.value)return
 if(note.value.trim().length<2){error.value='请填写至少2字的操作原因';return}
 const ticket=++generation,owner=user.value?.id;saving.value=true;error.value=''
 try{await api(`/admin/users/${props.member.id}/inviter`,'POST',{revision:plan.value.revision,inviter_username:plan.value.new_inviter_username,detach:!plan.value.new_inviter_id,note:note.value.trim(),key:requestKey.value});if(ticket!==generation||user.value?.id!==owner)return;emit('notify','上级已更改，团队归属与统计已同步');emit('saved');emit('close')}
 catch(e){if(ticket===generation)error.value=e instanceof Error?e.message:'更改失败，请重试或重新预览'}
 finally{if(ticket===generation)saving.value=false}
}
function close(){if(!saving.value)emit('close')}
function trap(e:KeyboardEvent){if(e.key!=='Tab')return;const els=dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),textarea:not(:disabled)');if(!els?.length)return;const first=els[0],last=els[els.length-1];if(e.shiftKey&&(document.activeElement===first||document.activeElement===dialog.value)){e.preventDefault();last?.focus()}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus()}}
watch(()=>[user.value?.id,props.member.id],()=>{saving.value=false;requestKey.value=key();void load()},{immediate:true})
onMounted(async()=>{previousFocus=document.activeElement as HTMLElement;await nextTick();dialog.value?.focus()})
onUnmounted(()=>{generation++;previousFocus?.focus()})
</script>
<template>
 <div class="modal-overlay" @click.self="close" @keydown.esc="close" @keydown="trap"><section ref="dialog" class="modal inviter-editor" role="dialog" aria-modal="true" aria-labelledby="inviter-editor-title" tabindex="-1">
  <button class="modal-close icon-button" :disabled="saving" aria-label="关闭更改上级" @click="close"><X/></button><h2 id="inviter-editor-title">更改上级 · {{member.username}}</h2>
  <p v-if="error" role="alert" class="form-error">{{error}}</p><button class="text-button" :disabled="loading||saving" @click="load">重新读取</button><p v-if="loading" role="status">正在读取关系…</p>
  <form v-if="state" class="form-stack" @submit.prevent="save">
   <p>当前上级：<strong>{{state.inviter_username||'无上级'}}</strong></p>
   <label>新上级账号<input v-model="parentName" :disabled="saving||loading||detach" :required="!detach" maxlength="32" autocomplete="off" placeholder="输入完整账号，不是邀请码"/></label>
   <label class="inviter-detach"><input v-model="detach" type="checkbox" :disabled="saving||loading"/>设为无上级（顶层团队）</label>
   <button type="button" class="secondary-button" :disabled="saving||loading" @click="preview">预览变更</button>
   <section v-if="plan" class="inviter-preview" aria-label="上级变更预览"><p>{{plan.inviter_username||'无上级'}} → <strong>{{plan.new_inviter_username||'无上级'}}</strong></p><p>迁移 {{member.username}} 及其 {{plan.descendants}} 名下级。</p></section>
   <p class="form-hint">整支下级跟随迁移，团队人数、累计充值和今日流水按新关系更新。不改余额、密码、历史订单或已生成佣金；已有订单仍按原上级快照结算，今后新订单按新关系记录。</p>
   <label>操作原因<textarea v-model="note" required minlength="2" maxlength="500" rows="2" :disabled="saving||loading"/></label><button class="primary-button" :disabled="saving||loading||!plan">{{saving?'更改中…':'确认更改上级'}}</button>
  </form>
 </section></div>
</template>
<style scoped>
.inviter-editor{text-align:left;max-height:90dvh;overflow:auto}.inviter-editor h2{font-size:21px;padding-right:24px;overflow-wrap:anywhere}.inviter-editor .text-button{margin:14px 0;color:#8a64b4}.inviter-editor .form-stack>p{font-size:13px;line-height:1.8}.inviter-detach{display:flex!important;align-items:center;gap:8px}.inviter-detach input{width:16px!important;height:16px}.inviter-preview{padding:12px;background:#f7f3fc;border-radius:7px;font-size:13px}.inviter-preview p{margin:3px 0}
</style>
