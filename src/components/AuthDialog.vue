<script setup lang="ts">
import {ref,computed,onMounted,onUnmounted,nextTick,watch} from 'vue'
import {ChevronLeft,ShieldCheck,ArrowRight,RefreshCw} from 'lucide-vue-next'
import {api,user,refreshAccount,site,type User} from '../api/account'
const emit=defineEmits<{close:[];success:[]}>()
const register=ref(!!new URLSearchParams(location.search).get('invite')),username=ref(''),password=ref(''),confirm=ref(''),phone=ref(''),name=ref(''),invite=ref(new URLSearchParams(location.search).get('invite')??''),error=ref(''),busy=ref(false),dialog=ref<HTMLElement|null>(null)
interface CaptchaChallenge {id:string;image_data_url:string;expires_at:string}
const wechat=ref(''),withdrawalPassword=ref(''),captchaAnswer=ref(''),captcha=ref<CaptchaChallenge|null>(null),captchaLoading=ref(false),captchaError=ref('')
const locked=computed(()=>busy.value)
let previous:HTMLElement|null=null,previousOverflow='',mounted=true
onMounted(async()=>{
 previous=document.activeElement as HTMLElement
 previousOverflow=document.body.style.overflow;document.body.style.overflow='hidden'
 if(register.value)void loadCaptcha()
 await nextTick();dialog.value?.querySelector('input')?.focus()
})
onUnmounted(()=>{mounted=false;document.body.style.overflow=previousOverflow;if(previous?.isConnected)previous.focus()})
watch(register,next=>{if(next&&(!captcha.value||Date.parse(captcha.value.expires_at)<=Date.now()))void loadCaptcha()})
watch(locked,next=>{if(next)void nextTick(()=>dialog.value?.focus())})
async function loadCaptcha(){
 if(captchaLoading.value||busy.value)return
 captchaLoading.value=true;captchaError.value='';captcha.value=null;captchaAnswer.value=''
 try{const value=await api<CaptchaChallenge>('/auth/captcha');if(mounted)captcha.value=value}
 catch(e){if(mounted)captchaError.value=e instanceof Error?e.message:'验证码读取失败，请点击图片重试'}
 finally{captchaLoading.value=false}
}
function close(){if(!locked.value)emit('close')}
function switchMode(next:boolean){if(locked.value||next===register.value||next&&!site.value?.registration)return;register.value=next;error.value=''}
function switchTab(e:KeyboardEvent){
 if(!['ArrowLeft','ArrowRight','Home','End'].includes(e.key)||locked.value)return
 e.preventDefault()
 const next=e.key==='Home'?false:e.key==='End'?true:!register.value
 switchMode(next)
 void nextTick(()=>dialog.value?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')?.focus())
}
function trap(e:KeyboardEvent){
 if(e.key!=='Tab')return
 const nodes=Array.from(dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled):not([tabindex="-1"]),input:not(:disabled)')??[]).filter(node=>node.getClientRects().length>0)
 if(!nodes.length){e.preventDefault();return}
 const first=nodes[0]!,last=nodes[nodes.length-1]!,outside=!dialog.value?.contains(document.activeElement),atDialog=document.activeElement===dialog.value
 if(e.shiftKey&&(outside||atDialog||document.activeElement===first)){e.preventDefault();last.focus()}
 else if(!e.shiftKey&&(outside||atDialog||document.activeElement===last)){e.preventDefault();first.focus()}
}
async function finishAuthentication(authenticated:User,registering:boolean){
 if(!authenticated?.id||!authenticated.username)throw new Error('登录状态读取失败，请重新登录')
 user.value=authenticated;password.value='';confirm.value=''
 if(registering){captcha.value=null;captchaAnswer.value='';withdrawalPassword.value=''}
 try{await refreshAccount()}
 catch{
  if(user.value?.id!==authenticated.id){register.value=false;error.value=registering?'账户已注册，请使用刚才的用户名和密码重新登录':'登录状态已失效，请重新登录';return}
 }
 emit('success')
}
async function submit(){
 if(locked.value)return
 error.value=''
 if(register.value){
  if(!/^[A-Za-z0-9]{4,32}$/.test(username.value)){error.value='用户名须为4–32位字母或数字，不支持下划线';return}
  if(password.value.length<10||password.value.length>128||!/[A-Z]/.test(password.value)||!/[a-z]/.test(password.value)){error.value='登录密码须为10–128位，且同时包含大写和小写字母';return}
  if(!name.value.trim()){error.value='请填写姓名';return}
  if(!wechat.value.trim()){error.value='请填写微信号';return}
  if(!phone.value.trim()){error.value='请填写手机号';return}
  if(!/^(1[3-9][0-9]{9}|\+[1-9][0-9]{6,14})$/.test(phone.value.trim())){error.value='请输入11位手机号，或带+国家区号的手机号码';return}
  if(!invite.value.trim()){error.value='注册需要邀请码，请填写有效邀请码';return}
  if(password.value!==confirm.value){error.value='两次输入的密码不一致';return}
  if(!/^[0-9]{6}$/.test(withdrawalPassword.value)){error.value='提现密码须为6位数字';return}
  if(!captcha.value||Date.parse(captcha.value.expires_at)<=Date.now()){error.value='请填写新的图形验证码';void loadCaptcha();return}
  if(!/^[A-Za-z0-9]{5}$/.test(captchaAnswer.value.trim())){error.value='请输入5位图形验证码';return}
 }
 busy.value=true
 const registering=register.value
 let registrationFailed=false
 try{
  const authenticated=await api<User>(registering?'/auth/register':'/auth/login','POST',registering?{username:username.value,password:password.value,invite_code:invite.value.trim(),phone:phone.value.trim(),name:name.value.trim(),wechat:wechat.value.trim(),withdrawal_password:withdrawalPassword.value,captcha_id:captcha.value!.id,captcha_answer:captchaAnswer.value.trim()}:{username:username.value,password:password.value})
  if(mounted)await finishAuthentication(authenticated,registering)
 }catch(e){
  registrationFailed=registering
  if(mounted)error.value=e instanceof Error?e.message:'连接失败'
 }finally{
  busy.value=false
  if(registrationFailed&&mounted)void loadCaptcha()
  if(mounted&&!registering&&error.value){password.value='';void nextTick(()=>dialog.value?.querySelector<HTMLInputElement>('input[aria-label="密码"]')?.focus())}
 }
}
</script>

<template>
 <div class="auth-overlay" @click.self="close" @keydown.esc.stop.prevent="close" @keydown="trap">
  <section ref="dialog" class="auth-panel" :class="{'auth-register':register}" role="dialog" aria-modal="true" aria-labelledby="auth-title" tabindex="-1">
   <header class="auth-header">
    <button class="auth-back" type="button" :aria-label="register?'关闭注册':'关闭登录'" :disabled="busy" @click="close"><ChevronLeft :size="27"/></button>
    <h2 id="auth-title">登录 / 注册</h2>
    <div class="auth-identity" aria-label="JS 金沙"><strong>JS</strong><span>金沙</span></div>
   </header>
   <div class="auth-body">
    <div class="auth-tabs" role="tablist" aria-label="账户登录或注册" @keydown="switchTab">
     <button id="auth-login-tab" type="button" role="tab" aria-controls="auth-form" :aria-selected="!register" :tabindex="register?-1:0" :class="{active:!register}" :disabled="busy" @click="switchMode(false)">登录</button>
     <button v-if="site?.registration||register" id="auth-register-tab" type="button" role="tab" aria-controls="auth-form" :aria-selected="register" :tabindex="register?0:-1" :class="{active:register}" :disabled="busy" @click="switchMode(true)">注册</button>
    </div>
    <form id="auth-form" class="auth-form" role="tabpanel" :aria-labelledby="register?'auth-register-tab':'auth-login-tab'" :aria-busy="busy" @submit.prevent="submit">
     <fieldset :disabled="busy">
      <legend class="auth-sr-only">{{register?'注册金沙账户':'登录金沙账户'}}</legend>
      <label class="auth-field"><span class="auth-label">用户名 <b aria-hidden="true">*</b></span><input v-model="username" aria-label="用户名" required :pattern="register?'[A-Za-z0-9]{4,32}':'[A-Za-z0-9_]{4,32}'" minlength="4" maxlength="32" autocomplete="username" autocapitalize="none" :spellcheck="false" :placeholder="register?'4–32位字母或数字':'请输入用户名'" :title="register?'用户名仅支持4–32位字母或数字，不支持下划线':undefined"/></label>
      <label class="auth-field"><span class="auth-label">密码 <b aria-hidden="true">*</b></span><input v-model="password" aria-label="密码" type="password" required minlength="10" maxlength="128" :autocomplete="register?'new-password':'current-password'" :pattern="register?'(?=.*[A-Z])(?=.*[a-z]).{10,128}':undefined" :aria-describedby="register?'auth-password-hint':undefined" :title="register?'10–128位，必须同时包含大写和小写字母':undefined" :placeholder="register?'10–128位，须含大写和小写字母':'请输入密码'"/></label>
      <template v-if="register">
       <p id="auth-password-hint" class="auth-hint auth-password-hint">登录密码须为10–128位，必须同时包含大写和小写字母。</p>
       <label class="auth-field"><span class="auth-label">确认密码 <b aria-hidden="true">*</b></span><input v-model="confirm" aria-label="确认密码" type="password" required minlength="10" maxlength="128" autocomplete="new-password" placeholder="再次输入密码"/></label>
       <div class="auth-field"><label class="auth-label" for="auth-captcha">验证码 <b aria-hidden="true">*</b></label><div class="auth-captcha-entry"><input id="auth-captcha" v-model="captchaAnswer" aria-label="验证码" required pattern="[A-Za-z0-9]{5}" minlength="5" maxlength="5" autocomplete="off" autocapitalize="characters" :spellcheck="false" aria-describedby="auth-captcha-hint" placeholder="5位图形码"/><button type="button" class="auth-captcha-image" :disabled="captchaLoading||busy" aria-label="刷新图形验证码" @click="loadCaptcha"><img v-if="captcha" :src="captcha.image_data_url" alt="图形验证码"/><span v-else>{{captchaLoading?'加载中…':'点击重试'}}</span><RefreshCw :size="12"/></button></div><p id="auth-captcha-hint" class="auth-field-hint">不区分大小写，5分钟有效。点击图片换一张。</p><p v-if="captchaError" class="auth-captcha-error" role="alert">{{captchaError}}</p></div>
       <label class="auth-field"><span class="auth-label">姓名 <b aria-hidden="true">*</b></span><input v-model="name" aria-label="姓名" required autocomplete="name" maxlength="40" placeholder="请输入姓名"/></label>
       <label class="auth-field"><span class="auth-label">手机号 <b aria-hidden="true">*</b></span><input v-model="phone" aria-label="手机号" type="tel" required autocomplete="tel" inputmode="tel" maxlength="16" placeholder="11位手机号，或+国家区号号码"/></label>
       <label class="auth-field"><span class="auth-label">提现密码 <b aria-hidden="true">*</b></span><input v-model="withdrawalPassword" aria-label="提现密码" type="password" inputmode="numeric" required pattern="[0-9]{6}" minlength="6" maxlength="6" autocomplete="new-password" placeholder="请设置6位数字提现密码"/></label>
       <label class="auth-field"><span class="auth-label">微信号 <b aria-hidden="true">*</b></span><input v-model="wechat" aria-label="微信号" required maxlength="64" autocomplete="off" placeholder="请输入微信号"/></label>
       <label class="auth-field"><span class="auth-label">邀请码 <b aria-hidden="true">*</b></span><input v-model="invite" aria-label="邀请码" required maxlength="12" aria-describedby="auth-invite-hint" placeholder="请输入邀请码"/></label>
      </template>
     </fieldset>
     <p v-if="register" id="auth-invite-hint" class="auth-hint">注册需填写有效邀请码。邀请码关联邀请人，注册后不可更改。</p>
     <div v-if="error" class="auth-error" role="alert">{{error}}</div>
     <button class="auth-submit" type="submit" :disabled="locked||(register&&(!captcha||captchaLoading))"><span>{{busy?'正在提交…':register?'注册并登录':'登录'}}</span><ArrowRight :size="19"/></button>
    </form>
    <p class="auth-footer"><ShieldCheck :size="15"/><span>JS 金沙 · 账户服务</span></p>
   </div>
  </section>

 </div>
</template>

<style scoped>
.auth-overlay{position:fixed;inset:0;z-index:100;display:flex;align-items:center;justify-content:center;padding:28px;background:#21113799;backdrop-filter:blur(7px);overscroll-behavior:contain}
.auth-panel{display:flex;flex-direction:column;width:510px;max-width:100%;max-height:calc(100dvh - 56px);overflow:hidden;border:1px solid #bd8ff54d;border-radius:20px;outline:none;background:linear-gradient(155deg,#8248c5 0%,#9051cc 52%,#7540b6 100%);color:#fff;box-shadow:0 26px 80px #1e0a4966}
.auth-header{display:grid;grid-template-columns:64px minmax(0,1fr) 64px;align-items:center;flex-shrink:0;min-height:66px;padding:4px 13px;background:linear-gradient(110deg,#5b42ed 0%,#7428e9 58%,#881ee5 100%);border-bottom:1px solid #d0b0ff24}
.auth-header h2{margin:0;text-align:center;color:#fff;font-size:19px;font-weight:600;letter-spacing:1px}
.auth-back{display:grid;place-items:center;width:42px;height:42px;padding:0;border-radius:10px;color:#f8f2ff}
.auth-back:hover:not(:disabled){background:#ffffff13}
.auth-identity{display:flex;align-items:baseline;justify-content:flex-end;gap:4px;white-space:nowrap;color:#fff0bf}
.auth-identity strong{font:italic 700 22px Georgia,serif;letter-spacing:-1px}.auth-identity span{font-size:12px;font-weight:500}
.auth-body{min-height:0;overflow-y:auto;padding:0 32px 25px;overscroll-behavior:contain;scrollbar-color:#c5a0ef #6c38a5}
.auth-tabs{display:flex;justify-content:center;gap:62px;padding:0 12px;margin-bottom:25px;border-bottom:1px solid #dbc0f325}
.auth-tabs button{position:relative;min-width:86px;padding:19px 12px 17px;font-size:18px;font-weight:600;color:#e1c9f5;letter-spacing:2px}
.auth-tabs button.active{color:#fff}.auth-tabs button.active:after{content:'';position:absolute;bottom:-1px;left:9px;right:9px;height:3px;border-radius:5px;background:#bdb0ff;box-shadow:0 0 12px #e2caff50}
.auth-form{margin:0}.auth-form fieldset{display:grid;gap:15px;min-width:0;margin:0;padding:0;border:0}
.auth-field{display:grid;grid-template-columns:80px minmax(0,1fr);align-items:center;gap:9px;min-width:0}
.auth-label{display:flex;align-items:center;gap:4px;color:#fff2ff;font-size:13px;white-space:nowrap}.auth-label b{color:#ff9cba;font-size:13px;font-weight:600}.auth-label small{color:#dcc1ed;font-size:9px}
.auth-field input,.auth-field select{display:block;width:100%;min-width:0;height:47px;padding:0 13px;border:1px solid #b791d638;border-radius:7px;outline:none;background:#583477a6;box-shadow:inset 0 1px 3px #36135112;color:#fff;font-size:14px;transition:border-color .15s,box-shadow .15s}
.auth-field input::placeholder{color:#cfb4e0;font-size:12px;opacity:1}.auth-field input:focus,.auth-field select:focus{border-color:#d5b8ff;box-shadow:0 0 0 3px #cba2ff20}.auth-field input:disabled,.auth-field select:disabled{opacity:.65;cursor:not-allowed}.auth-field select{font-size:12px;text-overflow:ellipsis}.auth-field select option{background:#583477;color:#fff}.auth-question-retry{padding:0 2px;text-decoration:underline;color:#fff}
.auth-field input:-webkit-autofill,.auth-field input:-webkit-autofill:hover,.auth-field input:-webkit-autofill:focus{-webkit-text-fill-color:#fff;caret-color:#fff;-webkit-box-shadow:0 0 0 1000px #5f3b81 inset}
.auth-captcha-entry{display:flex;align-items:center;gap:7px;min-width:0}.auth-captcha-entry>input{flex:1;min-width:0}.auth-captcha-image{display:flex;align-items:center;justify-content:center;gap:3px;width:115px;height:43px;flex-shrink:0;padding:3px;border:1px solid #c9acd8;border-radius:5px;background:#f4edff;color:#71409a;overflow:hidden}.auth-captcha-image img{display:block;width:95px;height:32px;object-fit:contain}.auth-captcha-image span{font-size:11px}.auth-captcha-image>svg{flex-shrink:0}.auth-field-hint,.auth-captcha-error{grid-column:2;margin:0;color:#e5cef5;font-size:10px;line-height:1.7}.auth-captcha-error{color:#ffe0e9}
.auth-password-hint{margin-top:0!important}
.auth-hint{margin:13px 0 0 89px;color:#e5cef5;font-size:10px;line-height:1.8}.auth-error{margin-top:17px;padding:11px 13px;border:1px solid #ffbed43d;border-radius:7px;background:#43234d73;color:#ffe0e9;font-size:12px;line-height:1.7;overflow-wrap:anywhere}
.auth-submit{display:flex;align-items:center;justify-content:center;gap:12px;width:100%;min-height:46px;margin-top:25px;border:1px solid #c4b5ff66;border-radius:8px;background:linear-gradient(105deg,#aa8bed,#9877e4 55%,#8062d4);box-shadow:0 5px 13px #38116a25;color:#fff;font-size:16px;font-weight:600;letter-spacing:1px}.auth-submit:hover:not(:disabled){background:linear-gradient(105deg,#b89af8,#a586ed 55%,#8d70de)}
.auth-footer{display:flex;align-items:center;justify-content:center;gap:6px;margin:22px 0 0;color:#dbc1ef;font-size:10px;letter-spacing:.5px}
.auth-panel button:disabled{opacity:.55;cursor:not-allowed}.auth-panel button:focus-visible{outline:2px solid #f3ddff;outline-offset:3px}.auth-sr-only{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}
@media(max-width:640px){
 .auth-overlay{display:block;padding:0;background:#8447c5;backdrop-filter:none}
 .auth-panel{width:100%;max-width:none;height:100dvh;max-height:none;border:0;border-radius:0;box-shadow:none;background:linear-gradient(145deg,#7d42c5 0%,#9455ce 58%,#8044c0 100%)}
 .auth-header{grid-template-columns:58px minmax(0,1fr) 58px;min-height:58px;padding:env(safe-area-inset-top,0px) 9px 0}.auth-header h2{font-size:18px}.auth-identity strong{font-size:21px}.auth-identity span{font-size:11px}
 .auth-body{flex:1;padding:0 20px calc(28px + env(safe-area-inset-bottom,0px))}.auth-tabs{gap:54px;margin-bottom:25px}.auth-tabs button{min-width:90px;padding:21px 10px 17px;font-size:19px}
 .auth-form fieldset{gap:16px}.auth-field{grid-template-columns:76px minmax(0,1fr);gap:6px}.auth-label{font-size:13px}.auth-field input,.auth-field select{height:48px;padding:0 11px;font-size:16px}.auth-field select{font-size:12px}.auth-field input::placeholder{font-size:11px}.auth-hint{margin-left:82px;font-size:10px}.auth-submit{margin-top:27px;min-height:47px}.auth-footer{margin-top:25px}
}
@media(max-width:360px){.auth-body{padding-left:14px;padding-right:14px}.auth-field{grid-template-columns:72px minmax(0,1fr);gap:4px}.auth-label{font-size:12px;gap:3px}.auth-hint{margin-left:76px}.auth-field input{padding-left:9px;padding-right:9px}.auth-field input::placeholder{font-size:10px}.auth-captcha-image{width:101px}.auth-captcha-image img{width:81px}}
</style>
