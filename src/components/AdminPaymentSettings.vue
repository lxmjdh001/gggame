<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { RefreshCw, Wallet } from 'lucide-vue-next'
import { api, cents } from '../api/account'
interface PaymentSettings {version:number;enabled:boolean;network:string;address:string;rate_yuan:string;min_amount_cents:number;max_amount_cents:number}
const emit=defineEmits<{notify:[message:string]}>()
const config=ref<PaymentSettings|null>(null),minimum=ref(''),maximum=ref(''),loading=ref(false),saving=ref(false),error=ref('')
function apply(next:PaymentSettings){config.value={...next,network:next.network??'',address:next.address??'',rate_yuan:next.rate_yuan??'',min_amount_cents:next.min_amount_cents??0,max_amount_cents:next.max_amount_cents??0};minimum.value=next.min_amount_cents?(next.min_amount_cents/100).toFixed(2):'';maximum.value=next.max_amount_cents?(next.max_amount_cents/100).toFixed(2):''}
async function load(){loading.value=true;error.value='';try{apply(await api<PaymentSettings>('/admin/payment-settings'))}catch(e){error.value=(e as Error).message}finally{loading.value=false}}
async function save(){if(!config.value||saving.value)return;saving.value=true;error.value='';try{const next={...config.value,address:config.value.address.trim(),rate_yuan:config.value.rate_yuan.trim(),min_amount_cents:minimum.value.trim()?cents(minimum.value):0,max_amount_cents:maximum.value.trim()?cents(maximum.value):0};apply(await api<PaymentSettings>('/admin/payment-settings','PUT',next));emit('notify','USDT收款设置已保存')}catch(e){error.value=(e as Error).message}finally{saving.value=false}}
onMounted(()=>void load())
</script>
<template>
 <section class="payment-settings">
  <div class="payment-heading"><div><h2>收款设置</h2><p>配置 USDT 收款方式，申请提交后由后台核对到账。</p></div><button class="secondary-button" :disabled="loading||saving" @click="load"><RefreshCw :size="15"/>重新读取</button></div>
  <p v-if="error" role="alert" class="form-error">{{error}}</p><p v-if="loading" class="loading-line">正在读取收款设置…</p>
  <form v-if="config" class="surface payment-form" @submit.prevent="save">
   <div class="payment-method-heading"><Wallet :size="22"/><h3>USDT 收款</h3><span class="badge" :class="config.enabled?'active':'disabled'">{{config.enabled?'已开启':'未开启'}}</span></div>
   <label class="payment-switch"><input type="checkbox" v-model="config.enabled" :disabled="saving||loading"/>向用户开放 USDT 充值</label>
   <div class="payment-fields">
    <label>收款网络<select aria-label="收款网络" v-model="config.network" :disabled="saving||loading" :required="config.enabled"><option value="" disabled>选择网络</option><option value="TRC20">TRC20</option><option value="ERC20">ERC20</option></select></label>
    <label>汇率（1 USDT = 人民币元）<input v-model="config.rate_yuan" inputmode="decimal" placeholder="请输入汇率" :disabled="saving||loading" :required="config.enabled" maxlength="24"/></label>
    <label class="full-width">收款地址<input v-model="config.address" autocomplete="off" spellcheck="false" placeholder="填写所选网络的真实收款地址" :disabled="saving||loading" :required="config.enabled" maxlength="128"/></label>
    <label>最低充值金额（元）<input v-model="minimum" inputmode="decimal" placeholder="至少 1.00" :disabled="saving||loading" :required="config.enabled"/></label>
    <label>最高充值金额（元）<input v-model="maximum" inputmode="decimal" placeholder="最高 1,000,000.00" :disabled="saving||loading" :required="config.enabled"/></label>
   </div>
   <p class="form-hint">USDT 数量支持正整数，入账金额按提交时的汇率换算至分。提交申请不会自动入账，需在“资金审核”核对网络、收款地址和完整交易哈希后通过。</p>
   <p class="form-hint">修改设置只影响新申请；已提交申请保留原收款信息与汇率。停用后不再接受新的 USDT 申请。</p>
   <div class="payment-actions"><small>配置版本 {{config.version}}</small><button class="primary-button" :disabled="saving||loading">{{saving?'保存中…':'保存收款设置'}}</button></div>
  </form>
 </section>
</template>
<style scoped>
.payment-heading{display:flex;align-items:center;justify-content:space-between;gap:16px;margin:24px 0}.payment-heading h2{font-size:22px;margin:0 0 8px}.payment-heading p{font-size:13px;color:#93879f;margin:0}.payment-form{padding:28px;max-width:920px}.payment-method-heading{display:flex;align-items:center;gap:10px;margin-bottom:22px;color:#7661b2}.payment-method-heading h3{margin:0;color:#484057}.payment-method-heading .badge{margin-left:auto}.payment-switch{display:flex;align-items:center;gap:10px;font-size:14px;margin-bottom:24px}.payment-switch input{accent-color:#7654e7;width:17px;height:17px}.payment-fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:20px;margin-bottom:22px}.payment-fields label{display:flex;flex-direction:column;gap:9px;font-size:13px;color:#776b87;min-width:0}.payment-fields input,.payment-fields select{width:100%;min-width:0;border:1px solid #e4ddee;border-radius:9px;padding:12px;background:white;color:#463d53;outline-color:#8465de}.payment-fields .full-width{grid-column:1/-1}.payment-form .form-hint{line-height:1.8;margin:12px 0}.payment-actions{display:flex;align-items:center;justify-content:space-between;border-top:1px solid #eee8f6;padding-top:20px;margin-top:22px}.payment-actions small{color:#a097ad}@media(max-width:760px){.payment-heading{align-items:flex-start}.payment-heading h2{font-size:20px}.payment-heading p{font-size:11px;line-height:1.6}.payment-heading button{white-space:nowrap}.payment-form{padding:18px}.payment-fields{grid-template-columns:1fr;gap:16px}.payment-fields label{font-size:12px}.payment-form .form-hint{font-size:11px}}
</style>
