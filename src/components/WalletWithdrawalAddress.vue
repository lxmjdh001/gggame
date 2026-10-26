<script setup lang="ts">
import {computed,onUnmounted,ref,watch} from 'vue'
import {user} from '../api/account'
import {loadWithdrawalAddresses,saveWithdrawalAddress,type WithdrawalAddress} from '../api/wallet'
import {validWithdrawalAccount,withdrawalAccountTypes,withdrawalMethodForNetwork,withdrawalAccountLabel} from '../lib/withdrawalAccounts'
const props=defineProps<{network:string;address:string;locked:boolean}>()
const emit=defineEmits<{'update:network':[network:string];'update:address':[address:string];busy:[busy:boolean];notify:[message:string]}>()
const saved=ref<WithdrawalAddress[]>([]),editing=ref(true),loading=ref(false),saving=ref(false),loaded=ref(false),error=ref('')
const current=computed(()=>saved.value.find(item=>item.network===props.network))
const accountType=computed(()=>withdrawalMethodForNetwork(props.network))
const isUsdt=computed(()=>accountType.value==='usdt')
const addressLabel=computed(()=>isUsdt.value?'USDT收款地址':'收款账号')
let generation=0
function selectSaved(){if(props.locked)return;emit('update:address',current.value?.address??'');editing.value=!current.value?.address;error.value=''}
function changeNetwork(event:Event){if(props.locked||loading.value||saving.value)return;emit('update:network',(event.target as HTMLSelectElement).value)}
function changeAccountType(event:Event){if(props.locked||loading.value||saving.value)return;const method=(event.target as HTMLSelectElement).value;emit('update:network',method==='usdt'?'TRC20':method.toUpperCase())}
async function load(){
 const owner=user.value?.id,ticket=++generation
 saved.value=[];loaded.value=false;error.value='';loading.value=true
 if(!owner){loading.value=false;return}
 try{const result=await loadWithdrawalAddresses();if(ticket!==generation||user.value?.id!==owner)return;saved.value=result;loaded.value=true;selectSaved()}
 catch(e){if(ticket===generation)error.value=e instanceof Error?e.message:'提现地址读取失败'}
 finally{if(ticket===generation)loading.value=false}
}
async function save(){
 if(props.locked||saving.value||loading.value||!loaded.value||!user.value)return
 const address=props.address.trim(),network=props.network,owner=user.value.id,ticket=generation
 if(!validWithdrawalAccount(network,address)){error.value=isUsdt.value?'请填写与所选网络匹配的完整USDT收款地址':'请填写1–128位收款账号，不能含空白或控制字符';return}
 saving.value=true;error.value='';emit('busy',true)
 try{const result=await saveWithdrawalAddress(network,current.value?.version??0,address);if(ticket!==generation||user.value?.id!==owner)return;saved.value=[...saved.value.filter(item=>item.network!==network),result];if(props.network===network){emit('update:address',result.address);editing.value=false};emit('notify','收款账号已保存，下次自动使用')}
 catch(e){if(ticket===generation)error.value=e instanceof Error?e.message:'提现地址保存失败'}
 finally{if(ticket===generation){saving.value=false;emit('busy',false)}}
}
watch(()=>user.value?.id,()=>{editing.value=true;saving.value=false;emit('busy',false);if(!props.locked)emit('update:address','');void load()},{immediate:true})
watch(()=>props.network,selectSaved)
onUnmounted(()=>{generation++;emit('busy',false)})
</script>
<template>
 <section class="saved-withdrawal-address" aria-label="保存的提现收款账号">
  <label class="address-field"><span>账户类型</span><select :value="accountType" :disabled="locked||loading||saving" aria-label="提现账户类型" @change="changeAccountType"><option v-for="item in withdrawalAccountTypes" :key="item.value" :value="item.value">{{item.label}}</option></select></label>
  <label v-if="isUsdt" class="address-field"><span>USDT网络</span><select :value="network" :disabled="locked||loading||saving" aria-label="USDT提现收款网络" @change="changeNetwork"><option value="TRC20">TRC20</option><option value="ERC20">ERC20</option></select></label>
  <div v-if="!editing&&current?.address" class="saved-address"><span>{{addressLabel}}</span><div><code>{{address}}</code><small>已保存 · {{isUsdt?network:withdrawalAccountLabel(accountType)}}</small></div><button type="button" :disabled="locked||loading||saving" @click="editing=true;error=''">更改</button></div>
  <template v-else><label class="address-field"><span>{{addressLabel}}</span><input :value="address" :disabled="locked||loading||saving" required maxlength="128" autocomplete="off" spellcheck="false" :aria-label="addressLabel" :placeholder="isUsdt?'请输入所选网络的收款地址':'请输入收款账号'" @input="emit('update:address',($event.target as HTMLInputElement).value)"/></label><div class="address-actions"><button type="button" :disabled="locked||loading||saving||!loaded" @click="save">{{saving?'保存中…':'保存账号'}}</button><button v-if="current?.address" type="button" :disabled="locked||loading||saving" @click="selectSaved">取消更改</button></div></template>
  <p v-if="loading" role="status">正在读取已保存地址…</p><p v-if="error" class="address-error" role="alert">{{error}} <button type="button" :disabled="locked||loading||saving" @click="load">重新读取</button></p>
  <p class="address-hint">按账户类型分别保存，USDT按网络分别保存；更改不会修改已提交的提现申请。提交前请核对收款账号。</p>
 </section>
</template>
<style scoped>
.saved-withdrawal-address{margin:0 0 18px;font-size:12px;color:#81738e}.address-field,.saved-address{display:flex;align-items:center;gap:14px;padding:16px 0;border-bottom:1px solid #eee9f3}.address-field>span,.saved-address>span{flex-shrink:0;width:108px}.address-field input,.address-field select{flex:1;min-width:0;padding:12px 8px;border:1px solid #e5e2eb;border-radius:5px;background:#fff;color:#544d61;font-size:13px}.saved-address{align-items:flex-start}.saved-address>div{flex:1;min-width:0}.saved-address code{display:block;font:inherit;overflow-wrap:anywhere;line-height:1.8;user-select:all}.saved-address small{display:block;color:#659079;margin-top:6px}.saved-address button,.address-actions button,.address-error button{color:#8059bd;text-decoration:underline;white-space:nowrap}.address-actions{display:flex;gap:18px;justify-content:flex-end;margin:12px 0}.address-hint,.address-error{font-size:11px;line-height:1.8}.address-error{color:#b76878}button:disabled{opacity:.5;cursor:not-allowed}@media(max-width:760px){.address-field>span,.saved-address>span{width:82px}.address-field,.saved-address{gap:8px}.address-field input{font-size:16px}}
</style>
