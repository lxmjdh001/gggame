<script setup lang="ts">
import {ref,watch,onUnmounted} from 'vue'
import {ShieldCheck} from 'lucide-vue-next'
import {api,user} from '../api/account'
import {validSecondaryPassword,validSecondarySecret,type SecondaryPasswordStatus} from '../lib/secondaryPassword'
const emit=defineEmits<{notify:[message:string]}>()
const state=ref<SecondaryPasswordStatus|null>(null),loading=ref(true),busy=ref(false),error=ref('')
const loginPassword=ref(''),currentPassword=ref(''),newPassword=ref(''),confirmation=ref('')
let generation=0
function clearSecrets(){loginPassword.value='';currentPassword.value='';newPassword.value='';confirmation.value=''}
function apply(next:SecondaryPasswordStatus){state.value=next;if(user.value)user.value={...user.value,secondary_password_set:next.is_set,secondary_password_enabled:next.enabled}}
async function load(){
 const ticket=++generation,id=user.value?.id
 state.value=null;loading.value=true;error.value='';clearSecrets()
 if(!id){loading.value=false;return}
 try{const next=await api<SecondaryPasswordStatus>('/auth/secondary-password/status');if(ticket===generation&&user.value?.id===id)apply(next)}
 catch(e){if(ticket===generation)error.value=e instanceof Error?e.message:'二级密码状态读取失败'}
 finally{if(ticket===generation)loading.value=false}
}
watch(()=>user.value?.id,()=>{void load()},{immediate:true})
onUnmounted(()=>{generation++;clearSecrets()})
async function save(toggle=false){
 if(busy.value||!state.value)return
 const id=user.value?.id,ticket=generation,wasSet=state.value.is_set,enabled=state.value.enabled
 error.value=''
 if(!toggle&&!validSecondaryPassword(newPassword.value)){error.value='二级密码需1–8个字符，不要求大小写组合';return}
 if(!toggle&&newPassword.value!==confirmation.value){error.value='两次二级密码不一致';return}
 if(wasSet&&!currentPassword.value){error.value='请输入当前二级密码';return}
 if(wasSet&&!validSecondarySecret(currentPassword.value)){error.value='当前二级密码需1–8个字符，旧9–12字符密码可继续使用';return}
 if(!wasSet&&!loginPassword.value){error.value='首次设置请确认登录密码';return}
 busy.value=true
 try{
  const next=await api<SecondaryPasswordStatus>(toggle?'/auth/secondary-password/toggle':'/auth/secondary-password','POST',toggle?{password:currentPassword.value,enabled:!enabled}:{login_password:loginPassword.value,current_password:currentPassword.value,new_password:newPassword.value,confirm_password:confirmation.value})
  if(ticket!==generation||user.value?.id!==id)return
  apply(next);emit('notify',toggle?(next.enabled?'二级密码验证已开启，进入游戏时需验证':'二级密码验证已关闭'):wasSet?'二级密码已更新':'二级密码已设置并开启')
 }catch(e){if(ticket===generation)error.value=e instanceof Error?e.message:'保存失败，请重试'}
 finally{clearSecrets();busy.value=false}
}
</script>

<template>
 <section class="surface secondary-settings" aria-labelledby="secondary-settings-title">
  <header><ShieldCheck :size="22"/><div><h3 id="secondary-settings-title">二级密码</h3><p>为进入游戏增加一层保护，与登录密码、提现密码独立。</p></div></header>
  <p v-if="loading" role="status">正在读取二级密码设置…</p>
  <div v-if="error" class="form-error" role="alert">{{error}}<button v-if="!state&&!loading" type="button" class="secondary-button" @click="load">重新读取</button></div>
  <template v-if="state">
   <div class="secondary-status"><strong>{{!state.is_set?'尚未设置':state.enabled?'验证已开启':'验证已关闭'}}</strong><span>{{state.is_set?'设置密码后可随时开启或关闭':'首次保存后自动开启验证'}}</span></div>
   <p class="secondary-rules">开启后：每次登录后首次进入游戏、北京时间跨天或距上次验证满1小时，都需要输入二级密码。同一次登录内各游戏共用验证时效。</p>
   <form class="form-stack" @submit.prevent="save()">
    <fieldset :disabled="busy">
     <label v-if="!state.is_set">登录密码<input v-model="loginPassword" type="password" required maxlength="128" autocomplete="current-password" placeholder="首次设置时确认身份"/></label>
     <label v-else>当前二级密码<input v-model="currentPassword" type="password" required maxlength="24" autocomplete="current-password" placeholder="修改密码或切换开关时填写"/></label>
     <div v-if="state.is_set" class="secondary-toggle"><span>进入游戏二级验证：{{state.enabled?'开启':'关闭'}}</span><button type="button" role="switch" :aria-checked="state.enabled" aria-label="进入游戏二级密码验证" :disabled="busy||!currentPassword" @click="save(true)">{{state.enabled?'关闭验证':'开启验证'}}</button></div>
     <!-- maxlength counts UTF-16 units; validation counts Unicode codepoints. -->
     <label>{{state.is_set?'新二级密码':'设置二级密码'}}<input v-model="newPassword" type="password" required maxlength="16" autocomplete="new-password" placeholder="1–8个字符，不要求大小写组合" aria-describedby="secondary-password-rule"/></label>
     <label>确认二级密码<input v-model="confirmation" type="password" required maxlength="16" autocomplete="new-password" placeholder="请再次输入二级密码"/></label>
     <small id="secondary-password-rule">新二级密码为1–8个字符，不要求大小写组合；验证时区分大小写，旧9–12字符密码可继续验证。修改密码或重新开启后，需要重新验证。</small>
     <button class="primary-button" type="submit" :disabled="busy">{{busy?'正在保存…':state.is_set?'保存新二级密码':'设置并开启二级密码'}}</button>
    </fieldset>
   </form>
  </template>
 </section>
</template>

<style scoped>
.secondary-settings{margin:16px;border-radius:7px}.secondary-settings header{display:flex;gap:10px;align-items:flex-start;color:#7253cc}.secondary-settings header h3{margin:0;color:#393345}.secondary-settings header p,.secondary-rules{font-size:12px;line-height:1.8;color:#797383;margin:7px 0}.secondary-status{display:flex;flex-wrap:wrap;align-items:center;gap:10px;margin-top:20px}.secondary-status strong{color:#7253cc;font-size:14px}.secondary-status span{font-size:12px;color:#89818f}.secondary-rules{padding:12px;background:#f6f3fc;border-radius:7px;margin:12px 0 20px}.form-stack fieldset{display:grid;gap:15px;min-width:0;padding:0;border:0;margin:0}.form-stack label{display:grid;gap:8px;font-size:13px;color:#51495d}.form-stack input{width:100%;min-width:0;box-sizing:border-box}.form-stack small{font-size:12px;color:#89818f;line-height:1.8}.secondary-toggle{display:flex;flex-wrap:wrap;gap:12px;justify-content:space-between;align-items:center;padding:12px;background:#faf8fd;border:1px solid #eee6f8;border-radius:7px;font-size:12px}.secondary-toggle button{padding:8px 12px;border:1px solid #b8a5e4;border-radius:6px;color:#7253cc}.secondary-toggle button[aria-checked=true]{background:#7253cc;color:white}.secondary-settings button:disabled{opacity:.55;cursor:not-allowed}.secondary-settings .form-error button{margin-left:10px}
@media(max-width:760px){.secondary-settings{margin:12px}.form-stack input{font-size:16px}}
</style>
