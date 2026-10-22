<script setup lang="ts">
import {ref,onMounted,onUnmounted,nextTick} from 'vue'
import {X,RefreshCw} from 'lucide-vue-next'
import {api} from '../api/account'
const props=defineProps<{member:{id:string;username:string};action:'password'|'descendants'}>()
const emit=defineEmits<{close:[];saved:[];notify:[message:string]}>()
interface Preview {username:string;revision:string;total?:number;active?:number;disabled?:number}
const preview=ref<Preview|null>(null),password=ref(''),confirmation=ref(''),note=ref(''),error=ref(''),loading=ref(false),saving=ref(false),dialog=ref<HTMLElement|null>(null)
let active=true,previousFocus:HTMLElement|null=null
async function load(){if(saving.value)return;loading.value=true;error.value='';preview.value=null;password.value='';confirmation.value='';try{const value=await api<Preview>(`/admin/users/${props.member.id}/${props.action==='password'?'password':'descendants'}`);if(active)preview.value=value}catch(e){if(active)error.value=(e as Error).message}finally{if(active)loading.value=false}}
async function save(){
 if(!preview.value||loading.value||saving.value)return
 error.value=''
 if(props.action==='password'){
  if(password.value.length<10||password.value.length>128||!/[A-Z]/.test(password.value)||!/[a-z]/.test(password.value)){error.value='新登录密码需10–128位，且同时包含大写和小写字母';return}
  if(password.value!==confirmation.value){error.value='两次输入的新密码不一致';return}
 }
 saving.value=true
 try{
  const body={revision:preview.value.revision,note:note.value.trim()}
  await api(`/admin/users/${props.member.id}/${props.action==='password'?'password':'disable-descendants'}`,'POST',props.action==='password'?{...body,new_password:password.value,password_confirmation:confirmation.value}:body)
  password.value='';confirmation.value=''
  emit('notify',props.action==='password'?'会员登录密码已修改，原登录会话已退出':`已停用 ${preview.value.active} 名下级会员，已记录操作日志`)
  emit('saved');emit('close')
 }catch(e){error.value=(e as Error).message}finally{saving.value=false}
}
function close(){if(!saving.value)emit('close')}
function trap(e:KeyboardEvent){if(e.key!=='Tab')return;const els=dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),textarea:not(:disabled)');if(!els?.length)return;const first=els[0],last=els[els.length-1];if(e.shiftKey&&(document.activeElement===first||document.activeElement===dialog.value)){e.preventDefault();last?.focus()}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus()}}
onMounted(async()=>{previousFocus=document.activeElement as HTMLElement;await nextTick();dialog.value?.focus();void load()})
onUnmounted(()=>{active=false;password.value='';confirmation.value='';previousFocus?.focus()})
</script>
<template>
 <div class="modal-overlay" @click.self="close" @keydown.esc="close" @keydown="trap"><section ref="dialog" class="modal member-management" role="dialog" aria-modal="true" aria-labelledby="member-management-title" tabindex="-1">
  <button class="modal-close icon-button" :disabled="saving" aria-label="关闭会员管理操作" @click="close"><X/></button>
  <h2 id="member-management-title">{{action==='password'?'修改会员登录密码':'停用全部下级'}} · {{member.username}}</h2>
  <p v-if="error" class="form-error" role="alert">{{error}}</p>
  <button class="text-button" :disabled="loading||saving" @click="load"><RefreshCw :size="14"/>重新读取</button>
  <p v-if="loading" class="loading-line" role="status">正在读取会员信息…</p>
  <form v-if="preview" class="form-stack" @submit.prevent="save">
   <template v-if="action==='password'">
    <label>新登录密码<input v-model="password" type="password" autocomplete="new-password" required minlength="10" maxlength="128" :disabled="saving" placeholder="10–128位，须包含大写和小写字母"/></label>
    <label>确认新登录密码<input v-model="confirmation" type="password" autocomplete="new-password" required minlength="10" maxlength="128" :disabled="saving" placeholder="请再次输入新登录密码"/></label>
    <p class="form-hint">修改后，该会员所有设备的登录会话将退出。提现密码保持不变。</p>
   </template>
   <template v-else>
    <div class="member-team-counts"><p>下级会员共 <strong>{{preview.total}}</strong> 人</p><p>本次将停用 <strong>{{preview.active}}</strong> 人，已停用 {{preview.disabled}} 人</p></div>
    <p class="form-hint">包含该会员邀请关系下的所有层级；停用后下级登录会话立即退出，无法登录及提交新操作。{{member.username}} 本人的状态不变，余额和历史记录保留。</p>
   </template>
   <label>操作原因<textarea v-model="note" :disabled="saving" required minlength="2" maxlength="500" rows="3" placeholder="请输入操作原因"/></label>
   <button class="primary-button" :disabled="saving||action==='descendants'&&!preview.active">{{saving?'处理中…':action==='password'?'确认修改密码':!preview.active?'没有需要停用的下级':'确认停用 '+preview.active+' 名下级'}}</button>
  </form>
 </section></div>
</template>
<style scoped>
.member-management{text-align:left;max-height:90dvh;overflow:auto}.member-management h2{font-size:21px;padding-right:20px;overflow-wrap:anywhere}.member-management .text-button{display:flex;gap:6px;margin:16px 0}.member-management .form-hint{line-height:1.8}.member-team-counts{background:#faf7ff;padding:12px;font-size:14px;line-height:1.8}.member-team-counts p{margin:4px 0}
</style>
