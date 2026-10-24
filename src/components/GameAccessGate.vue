<script setup lang="ts">
import {ref,watch,onMounted,onUnmounted} from 'vue'
import {ShieldCheck,ChevronLeft} from 'lucide-vue-next'
import {api,user,authReady,gameAccessChallenge} from '../api/account'
import {validSecondaryPassword,gameAccessTTL,type SecondaryPasswordStatus} from '../lib/secondaryPassword'
defineProps<{pending?:boolean}>()
const emit=defineEmits<{back:[]}>()
const state=ref<SecondaryPasswordStatus|null>(null),unlocked=ref(false),hasEntered=ref(false),checking=ref(true),busy=ref(false),error=ref(''),password=ref('')
let generation=0,requestPending=false,mounted=true,deadline:ReturnType<typeof setTimeout>|undefined,poll:ReturnType<typeof setInterval>|undefined
function lock(){unlocked.value=false;clearTimeout(deadline)}
function apply(next:SecondaryPasswordStatus,elapsed=0){
 const ttl=Math.max(0,gameAccessTTL(next)-Math.max(0,elapsed))
 state.value=next;clearTimeout(deadline);unlocked.value=ttl>0
 if(unlocked.value)hasEntered.value=true
 if(Number.isFinite(ttl)&&ttl>0)deadline=setTimeout(()=>{lock();void check()},ttl)
}
async function check(){
 if(!mounted||!authReady.value||busy.value||requestPending)return
 const ticket=generation,id=user.value?.id,started=performance.now()
 if(!id){clearTimeout(deadline);state.value=null;unlocked.value=true;hasEntered.value=true;checking.value=false;return}
 requestPending=true
 if(!unlocked.value)checking.value=true
 try{const next=await api<SecondaryPasswordStatus>('/auth/secondary-password/status');if(ticket===generation&&user.value?.id===id){apply(next,performance.now()-started);error.value=''}}
 catch(e){if(ticket===generation){lock();error.value=e instanceof Error?e.message:'验证状态读取失败，请重试'}}
 finally{requestPending=false;if(ticket===generation)checking.value=false;else void check()}
}
watch([authReady,()=>user.value?.id,()=>user.value?.secondary_password_enabled,()=>user.value?.secondary_password_set,gameAccessChallenge],()=>{generation++;lock();password.value='';error.value='';void check()},{immediate:true})
async function verify(){
 if(busy.value)return
 if(!validSecondaryPassword(password.value)){error.value='请输入6–12位二级密码';return}
 const id=user.value?.id,ticket=++generation,started=performance.now()
 busy.value=true;error.value=''
 try{const next=await api<SecondaryPasswordStatus>('/auth/secondary-password/verify','POST',{password:password.value});if(ticket===generation&&user.value?.id===id)apply(next,performance.now()-started)}
 catch(e){if(ticket===generation){lock();error.value=e instanceof Error?e.message:'验证失败，请重试'}}
 finally{password.value='';busy.value=false;checking.value=false;if(ticket!==generation)void check()}
}
function visible(){if(document.visibilityState==='visible'){lock();void check()}}
onMounted(()=>{poll=setInterval(()=>{void check()},15000);document.addEventListener('visibilitychange',visible)})
onUnmounted(()=>{mounted=false;generation++;clearTimeout(deadline);clearInterval(poll);document.removeEventListener('visibilitychange',visible);password.value=''})
</script>

<template>
 <div class="game-access-gate">
  <!-- Keep a pending request and its idempotency key alive when verification expires. -->
  <div v-if="hasEntered" v-show="unlocked"><slot/></div>
  <section v-if="!unlocked" class="surface game-access-card" aria-labelledby="game-access-title" :aria-busy="checking||busy">
   <ShieldCheck class="access-shield" :size="34"/>
   <h2 id="game-access-title">{{checking?'正在检查游戏安全验证':'进入游戏前验证二级密码'}}</h2>
   <p>每次登录后首次进入游戏、北京时间跨天或验证满1小时，需重新验证。同一次登录内各游戏共用时效。</p>
   <div v-if="error" class="form-error" role="alert">{{error}}</div>
   <form v-if="!checking&&state?.enabled" class="form-stack" @submit.prevent="verify">
    <label>二级密码<input v-model="password" aria-label="二级密码" type="password" required maxlength="24" autocomplete="current-password" placeholder="请输入6–12位二级密码" :disabled="busy"/></label>
    <button class="primary-button" type="submit" :disabled="busy">{{busy?'正在验证…':'验证并进入游戏'}}</button>
   </form>
   <button v-if="!checking" type="button" class="secondary-button" :disabled="busy" @click="check">重新检查验证状态</button>
   <button type="button" class="access-back" :disabled="busy||pending" @click="emit('back')"><ChevronLeft :size="16"/>返回大厅</button>
   <small v-if="pending">原投注结果仍待确认，请完成验证后继续核对，勿重复下单。</small>
  </section>
 </div>
</template>

<style scoped>
.game-access-card{width:100%;max-width:480px;margin:30px auto;padding:28px;box-sizing:border-box;text-align:center}.access-shield{color:#7955d7;margin:0 auto 14px}.game-access-card h2{font-size:20px;color:#40324f}.game-access-card p,.game-access-card small{font-size:12px;color:#8c8197;line-height:1.9}.game-access-card .form-stack{margin:22px 0 15px;text-align:left}.form-stack label{display:grid;gap:8px;font-size:13px}.form-stack input{width:100%;box-sizing:border-box}.game-access-card .secondary-button{margin-top:8px}.access-back{display:flex;align-items:center;justify-content:center;gap:4px;margin:18px auto 0;color:#88769a;font-size:13px}.game-access-card small{display:block;margin-top:10px}.game-access-card button:disabled{opacity:.55;cursor:not-allowed}@media(max-width:760px){.game-access-card{padding:22px 18px;margin:18px auto}.form-stack input{font-size:16px}}
</style>
