<script setup lang="ts">
import {ref,computed} from 'vue'
import {Copy} from 'lucide-vue-next'
import {user} from '../api/account'
const emit=defineEmits<{notify:[message:string]}>()
const copyError=ref('')
const inviteURL=computed(()=>`${location.origin}/?invite=${encodeURIComponent(user.value?.invite_code??'')}`)
async function copy(value:string,label:string){
 copyError.value=''
 try{await navigator.clipboard.writeText(value);emit('notify',`${label}已复制`)}
 catch{copyError.value='无法自动复制，请长按选中后复制。'}
}
</script>
<template>
 <div class="invite-friends">
  <section class="invite-sharing" aria-label="邀请好友分享信息">
   <p class="invite-slogan">邀请好友，精彩共享</p>
   <div class="invite-code-row"><span>邀请码</span><strong>{{user?.invite_code}}</strong><button type="button" aria-label="复制邀请码" @click="copy(user?.invite_code??'','邀请码')"><Copy :size="16"/>复制</button></div>
   <label class="invite-link-label" for="friend-invite-link">邀请好友的链接</label>
   <div class="friend-invite-link"><input id="friend-invite-link" :value="inviteURL" readonly aria-label="邀请好友的链接" @focus="($event.target as HTMLInputElement).select()"/><button type="button" @click="copy(inviteURL,'邀请链接')">复制链接</button></div>
   <p v-if="copyError" class="invite-copy-error" role="alert">{{copyError}}</p>
  </section>
 </div>
</template>
<style scoped>
.invite-friends{color:#37343f;background:#fff}
.invite-sharing{padding:0 22px 22px}
.invite-slogan{padding:24px 0;color:#9b5573;font-size:23px;font-weight:500;border-bottom:1px solid #e9e7ed}
.invite-code-row{display:flex;align-items:center;gap:16px;min-height:82px;border-bottom:1px solid #e9e7ed;font-size:18px}
.invite-code-row strong{font-size:25px;letter-spacing:2px;font-weight:600;overflow-wrap:anywhere;min-width:0}
.invite-code-row button{margin-left:auto;display:flex;align-items:center;gap:5px;font-size:14px;color:#7353e8;flex-shrink:0;padding:8px 0}
.invite-link-label{display:block;margin:23px 0 13px;font-size:19px}
.friend-invite-link{display:flex;align-items:stretch;gap:8px;min-width:0}
.friend-invite-link input{flex:1;min-width:0;width:0;border:1px solid #e3deed;border-radius:4px;background:#faf9fc;height:44px;padding:0 10px;font-size:15px;color:#595366}
.friend-invite-link button{flex-shrink:0;background:#7353e8;color:#fff;border-radius:4px;font-size:14px;padding:0 13px}
.invite-copy-error{font-size:13px;color:#a44760;margin-top:9px}
@media(max-width:760px){.invite-sharing{padding:0 16px 22px}.invite-slogan{font-size:21px;padding:22px 0}.invite-code-row{gap:12px;min-height:78px;font-size:17px}.invite-code-row strong{font-size:24px}.invite-link-label{font-size:18px}.friend-invite-link input{font-size:14px}.friend-invite-link button{font-size:13px;padding:0 10px}}
@media(max-width:360px){.invite-code-row{gap:9px}.invite-code-row strong{font-size:21px}.invite-code-row button{font-size:12px}}
</style>
