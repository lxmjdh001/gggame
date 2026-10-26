<script setup lang="ts">
import {computed} from 'vue'
import {account,user,money} from '../api/account'
const team=computed(()=>account.value?.user.id===user.value?.id?account.value?.members.filter(member=>member.level===1)??[]:[])
</script>
<template>
 <section class="my-team" aria-label="直属下级团队">
  <p class="team-description">直属下级 {{team.length}} 人<span v-if="account?.user.id===user?.id&&account?.team_day"> · {{account.team_day}}（北京时间）</span></p>
  <table><thead><tr><th scope="col">用户名</th><th scope="col">账户余额（元）</th><th scope="col">当日下注流水（元）</th></tr></thead>
   <tbody><tr v-for="member in team" :key="member.id"><th scope="row">{{member.username}}</th><td>{{money(member.balance_cents)}}</td><td>{{money(member.today_stake_cents)}}</td></tr>
    <tr v-if="!team.length"><td colspan="3" class="team-empty">暂无直属下级</td></tr>
   </tbody>
  </table>
 </section>
</template>
<style scoped>
.my-team{background:#fff;color:#37343f}.team-description{margin:0;padding:18px 20px;font-size:13px;color:#81738e;line-height:1.8}.my-team table{width:100%;table-layout:fixed;border-collapse:collapse}.my-team th,.my-team td{padding:20px 16px;border-top:1px solid #e5e3ea;text-align:center;font-size:16px;font-weight:400;overflow-wrap:anywhere;font-variant-numeric:tabular-nums}.my-team thead th{background:#faf9fc;font-size:14px;font-weight:500}.my-team tbody th{text-align:left}.my-team .team-empty{font-size:14px;color:#8c8596;padding:34px 16px}@media(max-width:760px){.my-team th,.my-team td{padding:18px 8px;font-size:14px}.my-team thead th{font-size:12px}.team-description{padding:16px}}
</style>
