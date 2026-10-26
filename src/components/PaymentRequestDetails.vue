<script setup lang="ts">
import {money,type WalletRequest} from '../api/account'
import {withdrawalAccountLabel} from '../lib/withdrawalAccounts'
defineProps<{request:WalletRequest}>()
</script>
<template>
 <dl v-if="request.withdrawal||request.payment_method==='usdt'||request.transaction_id" class="payment-request-details">
  <template v-if="request.withdrawal">
   <div><dt>手续费（{{request.withdrawal.fee_bps/100}}%）</dt><dd>{{money(request.withdrawal.fee_cents)}} 元</dd></div>
   <div><dt>扣费后金额</dt><dd>{{money(request.withdrawal.net_cents)}} 元</dd></div>
   <template v-if="request.withdrawal.usdt_amount">
    <div><dt>账户类型</dt><dd>{{withdrawalAccountLabel(request.payment_method)}}</dd></div>
    <div v-if="request.payment_method==='usdt'"><dt>USDT网络</dt><dd>{{request.withdrawal.network}}</dd></div>
    <div><dt>到账数量</dt><dd>{{request.withdrawal.usdt_amount}} U</dd></div>
    <div><dt>申请汇率</dt><dd>1 U＝{{request.withdrawal.rate_yuan}} 元</dd></div>
    <div><dt>{{request.payment_method==='usdt'?'USDT收款地址':'收款账号'}}</dt><dd>{{request.withdrawal.address}}</dd></div>
   </template>
  </template>
  <template v-else-if="request.payment_method==='usdt'">
   <div><dt>USDT / 网络</dt><dd>{{request.usdt_amount}} USDT · {{request.payment_network}}</dd></div>
   <div><dt>申请汇率</dt><dd>1 USDT = {{request.rate_yuan}} 元</dd></div>
   <div><dt>收款地址</dt><dd>{{request.payment_address}}</dd></div>
  </template>
  <div v-if="request.transaction_id"><dt>交易 ID</dt><dd>{{request.transaction_id}}</dd></div>
 </dl>
</template>
<style scoped>.payment-request-details{margin:10px 0;display:grid;gap:8px;font-size:12px;color:#5b5368;line-height:1.6}.payment-request-details div{min-width:0}.payment-request-details dt{color:#9c92a8;font-size:11px}.payment-request-details dd{margin:0;overflow-wrap:anywhere;white-space:normal;user-select:text;font-variant-numeric:tabular-nums}</style>
