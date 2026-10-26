<script setup lang="ts">
import {computed,nextTick,onMounted,onUnmounted,ref,watch} from 'vue'
import {X} from 'lucide-vue-next'
import {api,key,money,user} from '../api/account'

const props=defineProps<{member:{id:string;username:string;role:string};action:'delete'|'restore'}>()
const emit=defineEmits<{close:[];saved:[];notify:[message:string]}>()
interface DeletionPreview {
 username:string;revision:string;deleted:boolean;balance_cents:number;frozen_cents:number;betting_frozen_cents:number
 available_commission_cents:number;pending_requests:number;pending_orders:number;direct_count:number;can_delete:boolean;blockers:string[]
}
const preview=ref<DeletionPreview|null>(null),confirmation=ref(''),note=ref(''),error=ref(''),loading=ref(false),saving=ref(false),dialog=ref<HTMLElement|null>(null)
const requestKey=ref('')
let active=true,generation=0,previousFocus:HTMLElement|null=null
const resource=()=>JSON.stringify([user.value?.id,user.value?.role,props.member.id,props.member.username,props.member.role,props.action])
const authorized=computed(()=>user.value?.role==='admin'&&props.member.role==='user'&&props.member.id!==user.value.id)
const blockers=computed(()=>{
 const p=preview.value
 if(!p)return []
 if(!authorized.value)return ['管理员及当前登录账户不可执行此操作']
 if(props.action==='restore')return p.deleted?[]:['该账号尚未删除，请重新读取']
 const reasons=[...p.blockers]
 if(p.deleted)reasons.push('该账号已删除，请切换至恢复用户')
 if(p.balance_cents!==0)reasons.push('可用余额不为零')
 if(p.frozen_cents!==0)reasons.push('提现冻结金额不为零')
 if(p.betting_frozen_cents!==0)reasons.push('投注冻结金额不为零')
 if(p.available_commission_cents!==0)reasons.push('存在未领取佣金')
 if(p.pending_requests!==0)reasons.push('存在待处理申请')
 if(p.pending_orders!==0)reasons.push('存在未结算投注')
 if(!p.can_delete&&!reasons.length)reasons.push('当前账号不可删除，请重新读取并核对')
 return [...new Set(reasons)]
})
const validForm=computed(()=>!!preview.value&&confirmation.value===preview.value.username&&Array.from(note.value.trim()).length>=2&&Array.from(note.value.trim()).length<=500)
function current(ticket:number,context:string){return active&&ticket===generation&&context===resource()}
function validPreview(value:DeletionPreview){
 return !!value&&typeof value.username==='string'&&!!value.username&&typeof value.revision==='string'&&!!value.revision&&typeof value.deleted==='boolean'&&typeof value.can_delete==='boolean'&&Array.isArray(value.blockers)&&value.blockers.every(reason=>typeof reason==='string')&&[value.balance_cents,value.frozen_cents,value.betting_frozen_cents,value.available_commission_cents,value.pending_requests,value.pending_orders,value.direct_count].every(Number.isSafeInteger)
}
async function load(){
 if(!active||loading.value||saving.value)return
 const ticket=++generation,context=resource(),id=props.member.id
 preview.value=null;confirmation.value='';note.value='';error.value='';requestKey.value=key()
 if(!authorized.value){error.value='管理员及当前登录账户不可执行此操作';return}
 loading.value=true
 try{
  const result=await api<DeletionPreview>(`/admin/users/${encodeURIComponent(id)}/deletion`)
  if(!current(ticket,context))return
  if(!validPreview(result))throw new Error('预览数据不完整，请重新读取')
  preview.value=result
 }catch(e){if(current(ticket,context))error.value=e instanceof Error?e.message:'读取失败，请重试'}
 finally{if(current(ticket,context))loading.value=false}
}
async function save(){
 if(!active||!authorized.value||!preview.value||loading.value||saving.value)return
 error.value=''
 if(blockers.value.length){error.value='当前预览不允许此操作，请核对阻止原因';return}
 if(confirmation.value!==preview.value.username){error.value='请输入与预览完全一致的账号（区分大小写，不可添加空格）';return}
 const reason=note.value.trim(),length=Array.from(reason).length
 if(length<2||length>500){error.value='操作原因需2–500字';return}
 const ticket=++generation,context=resource(),id=props.member.id,operation=props.action
 const body={revision:preview.value.revision,confirm_username:confirmation.value,note:reason,key:requestKey.value}
 saving.value=true
 dialog.value?.focus()
 try{
  await api(`/admin/users/${encodeURIComponent(id)}${operation==='restore'?'/restore':''}`,operation==='restore'?'POST':'DELETE',body)
  if(!current(ticket,context))return
  preview.value=null;confirmation.value='';note.value=''
  emit('notify',operation==='restore'?'用户已恢复，当前为停用状态，需再启用才能登录':'用户已删除，历史记录和下级关系保留')
  emit('saved');emit('close')
 }catch(e){
  if(!current(ticket,context))return
  error.value=(e instanceof Error?e.message:'操作失败')+'；请重新读取预览后重试'
  // Re-read authoritative state after failure, including an uncertain network outcome.
  preview.value=null;confirmation.value=''
 }finally{if(current(ticket,context))saving.value=false}
}
function close(){if(!saving.value){active=false;generation++;emit('close')}}
function trap(e:KeyboardEvent){
 if(e.key!=='Tab')return
 const elements=dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),textarea:not(:disabled)')
 if(!elements?.length){e.preventDefault();dialog.value?.focus();return}
 const first=elements[0],last=elements[elements.length-1]
 if(e.shiftKey&&(document.activeElement===first||document.activeElement===dialog.value)){e.preventDefault();last?.focus()}
 else if(!e.shiftKey&&(document.activeElement===last||document.activeElement===dialog.value)){e.preventDefault();first?.focus()}
}
watch(()=>resource(),()=>{
 generation++;loading.value=false;saving.value=false;preview.value=null;confirmation.value='';note.value='';error.value=''
 void load()
},{immediate:true,flush:'sync'})
onMounted(async()=>{previousFocus=document.activeElement as HTMLElement;await nextTick();if(active)dialog.value?.focus()})
onUnmounted(()=>{active=false;generation++;preview.value=null;confirmation.value='';note.value='';if(previousFocus?.isConnected)previousFocus.focus()})
</script>
<template>
 <div class="modal-overlay" @click.self="close" @keydown.esc.stop.prevent="close" @keydown="trap"><section ref="dialog" class="modal member-deletion" role="dialog" aria-modal="true" aria-labelledby="member-deletion-title" aria-describedby="member-deletion-hint" tabindex="-1" :aria-busy="loading||saving">
  <button class="modal-close icon-button" :disabled="saving" aria-label="关闭删除或恢复用户" @click="close"><X/></button>
  <h2 id="member-deletion-title">{{action==='restore'?'恢复用户':'删除用户'}} · {{member.username}}</h2>
  <p id="member-deletion-hint" class="form-hint">{{action==='restore'?'恢复后账号为停用状态，仍需在正常用户列表启用后才能登录。':'删除后账号隐藏并退出登录，历史记录和下级关系保留；账号名称保留，邀请码停止新注册，可在“已删除”列表恢复。'}}</p>
  <p v-if="error" class="form-error" role="alert">{{error}}</p>
  <button class="text-button" :disabled="loading||saving" @click="load">重新读取预览</button>
  <p v-if="loading" class="loading-line" role="status">正在读取账号删除状态…</p>
  <form v-if="preview" class="form-stack" @submit.prevent="save">
   <section class="deletion-preview" aria-label="账号操作预览">
    <p>确认账号：<strong>{{preview.username}}</strong> · {{preview.deleted?'已删除':'未删除'}}</p>
    <dl><div><dt>可用余额</dt><dd>{{money(preview.balance_cents)}}</dd></div><div><dt>提现冻结</dt><dd>{{money(preview.frozen_cents)}}</dd></div><div><dt>投注冻结</dt><dd>{{money(preview.betting_frozen_cents)}}</dd></div><div><dt>未领取佣金</dt><dd>{{money(preview.available_commission_cents)}}</dd></div><div><dt>待处理申请 / 未结算投注</dt><dd>{{preview.pending_requests}} / {{preview.pending_orders}}</dd></div><div><dt>保留直属下级</dt><dd>{{preview.direct_count}} 人</dd></div></dl>
   </section>
   <div v-if="action==='delete'&&blockers.length" class="form-error" role="alert"><strong>无法删除：</strong><ul><li v-for="reason in blockers" :key="reason">{{reason}}</li></ul></div>
   <p v-if="action==='restore'&&!preview.deleted" class="form-error" role="alert">该账号尚未删除，请重新读取。</p>
   <label>输入完整账号确认<input v-model="confirmation" :disabled="saving||!!blockers.length" required autocomplete="off" spellcheck="false" :placeholder="preview.username"/></label>
   <label>操作原因<textarea v-model="note" :disabled="saving||!!blockers.length" required minlength="2" maxlength="1000" rows="3" placeholder="请输入2–500字的操作原因"/></label>
   <button class="primary-button" :disabled="loading||saving||!!blockers.length||!validForm">{{saving?'处理中…':action==='restore'?'确认恢复为停用状态':'确认删除用户'}}</button>
  </form>
 </section></div>
</template>
<style scoped>
.member-deletion{text-align:left;max-height:90vh;max-height:90dvh;overflow:auto}.member-deletion h2{font-size:21px;padding-right:24px;overflow-wrap:anywhere}.member-deletion>.form-hint{margin:14px 0;line-height:1.8}.member-deletion .text-button{margin:0 0 16px;color:#8a64b4}.deletion-preview{padding:12px;background:#f7f3fc;border-radius:7px;font-size:13px;overflow-wrap:anywhere}.deletion-preview p{margin:0 0 10px}.deletion-preview dl{margin:0}.deletion-preview dl>div{display:flex;justify-content:space-between;gap:12px;line-height:1.9}.deletion-preview dd{margin:0;font-variant-numeric:tabular-nums}.member-deletion ul{margin:8px 0 0;padding-left:20px;font-size:12px;line-height:1.8}
</style>
