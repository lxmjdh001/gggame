<script setup lang="ts">
import {ref,computed} from 'vue'
import {Copy} from 'lucide-vue-next'
import {user,account,money} from '../api/account'
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
  <section class="invite-team" aria-label="我的团队">
   <table class="invite-team-table">
    <thead><tr><th scope="col">我的团队</th><th scope="col">账户余额</th><th scope="col">当日下注</th></tr></thead>
    <tbody>
     <tr v-for="member in account?.members" :key="member.id"><th scope="row">{{member.username}}</th><td>{{money(member.balance_cents)}}</td><td>{{money(member.today_stake_cents)}}</td></tr>
     <tr v-if="!account?.members.length"><td colspan="3" class="invite-team-empty">暂无团队成员，分享邀请链接邀请好友加入。</td></tr>
    </tbody>
   </table>
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
.invite-team-table{width:100%;table-layout:fixed;border-collapse:collapse}
.invite-team-table th,.invite-team-table td{padding:22px 16px;border-top:1px solid #e5e3ea;text-align:center;font-size:20px;font-weight:400;overflow-wrap:anywhere;font-variant-numeric:tabular-nums}
.invite-team-table thead th{background:#faf9fc;font-size:19px;font-weight:500}
.invite-team-table tbody th{text-align:left;padding-left:22px}
.invite-team-table tbody td{font-size:21px}
.invite-team-table .invite-team-empty{font-size:14px;color:#8c8596;line-height:1.8;padding:34px 20px}
@media(max-width:760px){.invite-sharing{padding:0 16px 22px}.invite-slogan{font-size:21px;padding:22px 0}.invite-code-row{gap:12px;min-height:78px;font-size:17px}.invite-code-row strong{font-size:24px}.invite-link-label{font-size:18px}.friend-invite-link input{font-size:14px}.friend-invite-link button{font-size:13px;padding:0 10px}.invite-team-table th,.invite-team-table td{padding:22px 8px;font-size:17px}.invite-team-table thead th{font-size:17px}.invite-team-table tbody th{padding-left:16px}.invite-team-table tbody td{font-size:19px}}
@media(max-width:360px){.invite-team-table thead th{font-size:15px}.invite-team-table tbody th{font-size:15px;padding-left:10px}.invite-team-table tbody td{font-size:16px}.invite-code-row{gap:9px}.invite-code-row strong{font-size:21px}.invite-code-row button{font-size:12px}}
</style>
