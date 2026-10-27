<script setup lang="ts">
import {computed,onUnmounted,ref,watch} from 'vue'
import {Check,ChevronLeft,ChevronRight,CreditCard,RefreshCw} from 'lucide-vue-next'
import {user} from '../api/account'
import {loadWithdrawalAddresses,saveWithdrawalAddress,type WithdrawalAddress} from '../api/wallet'
import {validWithdrawalAccount,withdrawalAccountTypes,withdrawalMethodForNetwork,withdrawalAccountLabel} from '../lib/withdrawalAccounts'
import {maskedDestination} from '../lib/withdrawalDisplay'
const props=defineProps<{active:boolean;locked:boolean;network:string}>()
const emit=defineEmits<{ready:[value:WithdrawalAddress];back:[];busy:[value:boolean];notify:[message:string];'setup-password':[]}>()
const screen=ref<'loading'|'manage'|'edit'|'error'>('loading'),saved=ref<WithdrawalAddress[]>([]),loading=ref(false),saving=ref(false),error=ref('')
const network=ref('TRC20'),address=ref(''),confirmation=ref(''),password=ref(''),adding=ref(true)
const accounts=computed(()=>saved.value.filter(item=>validWithdrawalAccount(item.network,item.address)))
const current=computed(()=>saved.value.find(item=>item.network===network.value))
const method=computed(()=>withdrawalMethodForNetwork(network.value))
const label=computed(()=>withdrawalAccountLabel(method.value))
const isUsdt=computed(()=>method.value==='usdt')
const addressLabel=computed(()=>isUsdt.value?'USDT地址':'收款账号')
let generation=0
function clearSecrets(){confirmation.value='';password.value=''}
async function load(automatic=false){
 const owner=user.value?.id,ticket=++generation
 saved.value=[];loading.value=true;error.value='';screen.value='loading';clearSecrets()
 if(!owner){loading.value=false;return}
 try{
  const result=await loadWithdrawalAddresses()
  if(ticket!==generation||user.value?.id!==owner)return
  saved.value=result;screen.value='manage'
  const selected=accounts.value.find(item=>item.network===props.network)??accounts.value[0]
  if(automatic&&selected&&!props.locked)emit('ready',selected)
 }catch(e){if(ticket===generation){screen.value='error';error.value=e instanceof Error?e.message:'收款账户读取失败'}}
 finally{if(ticket===generation)loading.value=false}
}
function add(){if(props.locked||loading.value||saving.value)return;adding.value=true;network.value='TRC20';address.value='';clearSecrets();error.value='';screen.value='edit'}
function edit(value:WithdrawalAddress){if(props.locked||saving.value)return;adding.value=false;network.value=value.network;address.value=value.address;clearSecrets();error.value='';screen.value='edit'}
function select(value:WithdrawalAddress){if(props.locked||saving.value)return;clearSecrets();emit('ready',value)}
function changeNetwork(value:string){if(props.locked||saving.value)return;network.value=value;address.value=adding.value?'':saved.value.find(item=>item.network===value)?.address??'';clearSecrets();error.value=''}
function changeMethod(event:Event){const method=(event.target as HTMLSelectElement).value;changeNetwork(method==='usdt'?'TRC20':method.toUpperCase())}
function back(){if(props.locked||saving.value)return;clearSecrets();error.value='';if(screen.value==='edit'){screen.value='manage';address.value=''}else if(accounts.value.length){const selected=accounts.value.find(item=>item.network===props.network)??accounts.value[0]!;select(selected)}else emit('back')}
async function save(){
 if(props.locked||loading.value||saving.value||!user.value)return
 error.value=''
 const destination=address.value.trim(),confirm=confirmation.value.trim()
 if(!validWithdrawalAccount(network.value,destination)){error.value=isUsdt.value?'请输入与所选网络匹配的完整USDT地址':'请输入1–128位收款账号，不能含空白或控制字符';return}
 if(destination!==confirm){error.value='两次地址或收款账号不一致';return}
 if(!/^\d{6}$/.test(password.value)){error.value='请输入6位数字提款密码（支付密码）';return}
 const owner=user.value.id,ticket=generation,target=network.value,pin=password.value,version=current.value?.version??0
 saving.value=true;emit('busy',true)
 try{
  const result=await saveWithdrawalAddress(target,version,destination,confirm,pin)
  if(ticket!==generation||user.value?.id!==owner)return
  saved.value=[...saved.value.filter(item=>item.network!==target),result];screen.value='manage';clearSecrets();emit('notify','收款账户已保存');emit('ready',result)
 }catch(e){if(ticket===generation)error.value=e instanceof Error?e.message:'收款账户保存失败'}
 finally{if(ticket===generation){password.value='';saving.value=false;emit('busy',false)}}
}
watch(()=>user.value?.id,()=>{generation++;saved.value=[];address.value='';clearSecrets();saving.value=false;emit('busy',false);void load(true)},{immediate:true,flush:'sync'})
watch(()=>props.active,(active,previous)=>{if(active&&previous===false){clearSecrets();void load(false)}else if(!active){clearSecrets();error.value=''}})
onUnmounted(()=>{generation++;clearSecrets();emit('busy',false)})
</script>
<template>
 <section v-show="active" class="withdrawal-accounts" :class="{'account-editor':screen==='edit'}" :aria-busy="loading||saving">
  <header><button type="button" :disabled="locked||saving" :aria-label="screen==='edit'?'返回账户管理':'返回个人中心'" @click="back"><ChevronLeft :size="26"/></button><h1>{{screen==='edit'?(adding?'添加':'更改')+label:'多卡管理'}}</h1><button type="button" :disabled="locked||loading||saving" aria-label="刷新收款账户" @click="load(false)"><RefreshCw :size="18"/></button></header>
  <p v-if="loading" class="account-message" role="status">正在读取收款账户…</p>
  <div v-else-if="screen==='error'" class="account-message" role="alert">{{error}} <button type="button" @click="load(true)">重新读取</button></div>
  <template v-else-if="screen==='manage'">
   <div class="manager-content"><button type="button" class="account-primary" :disabled="locked||saving" @click="add">新增账户</button>
    <article v-for="item in accounts" :key="item.network" class="destination-card"><button type="button" class="destination-select" :disabled="locked||saving" @click="select(item)"><CreditCard :size="23"/><span><strong>{{withdrawalAccountLabel(withdrawalMethodForNetwork(item.network))}}<small v-if="item.network==='TRC20'||item.network==='ERC20'"> · {{item.network}}</small></strong><small>{{maskedDestination(item.address)}}</small></span><Check v-if="item.network===props.network" :size="18"/><ChevronRight v-else :size="18"/></button><button type="button" class="edit-link" :disabled="locked||saving" @click="edit(item)">更改</button></article>
   </div>
  </template>
  <form v-else-if="screen==='edit'" class="account-editor-form" @submit.prevent="save">
   <label><span>钱包类型:</span><select :value="method" :disabled="locked||saving" aria-label="钱包类型" @change="changeMethod"><option v-for="item in withdrawalAccountTypes" :key="item.value" :value="item.value">{{item.label}}</option></select></label>
   <label v-if="isUsdt"><span>USDT网络:</span><select :value="network" :disabled="locked||saving" aria-label="USDT网络" @change="changeNetwork(($event.target as HTMLSelectElement).value)"><option value="TRC20">TRC20</option><option value="ERC20">ERC20</option></select></label>
   <label><span>{{addressLabel}}<i>*</i>:</span><input v-model="address" :disabled="locked||saving" required maxlength="128" autocomplete="off" spellcheck="false" :aria-label="addressLabel" :placeholder="isUsdt?'请输入USDT地址':'请输入收款账号'"/></label>
   <label><span>{{isUsdt?'确认地址':'确认账号'}}<i>*</i>:</span><input v-model="confirmation" :disabled="locked||saving" required maxlength="128" autocomplete="off" spellcheck="false" :aria-label="isUsdt?'确认地址':'确认账号'" :placeholder="isUsdt?'请再次输入USDT地址':'请再次输入收款账号'"/></label>
   <label><span>提款密码<i>*</i>:</span><input v-model="password" :disabled="locked||saving" required type="password" inputmode="numeric" pattern="[0-9]{6}" minlength="6" maxlength="6" autocomplete="off" aria-label="提款密码（支付密码）" placeholder="请输入支付密码"/></label>
   <p v-if="!user?.withdrawal_password_set" class="account-message">请先设置6位数字提款密码。<button type="button" :disabled="locked||saving" @click="clearSecrets();emit('setup-password')">设置提款密码</button></p>
   <p v-if="error" class="account-message error" role="alert">{{error}}</p>
   <button class="account-primary" :disabled="locked||saving||!user?.withdrawal_password_set">{{saving?'保存中…':'保存'}}</button>
   <p class="account-hint">{{current?.address?'此钱包类型及网络已有账户，保存后将替换收款信息。':'保存后进入提款页面，下次提款直接使用。'}}更改不会影响已提交的提款申请。</p>
  </form>
 </section>
</template>
<style scoped>
.withdrawal-accounts{min-height:100vh;min-height:100dvh;background:#f2f2f2;color:#272727;font-family:Arial,'PingFang SC','Microsoft YaHei',sans-serif;padding-bottom:28px}.withdrawal-accounts header{display:grid;grid-template-columns:42px 1fr 42px;align-items:center;min-height:52px;padding:0 12px;background:linear-gradient(110deg,#4f60f3,#8a52ed);color:#fff}.withdrawal-accounts header h1{font-size:19px;font-weight:400;text-align:center;margin:0}.withdrawal-accounts header button{display:grid;place-items:center;min-height:44px}.manager-content{padding:22px 24px}.account-primary{display:block;width:100%;min-height:48px;padding:12px 20px;border-radius:28px;background:linear-gradient(100deg,#8062eb,#8d59e6);color:#fff;font-size:17px}.destination-card{display:flex;align-items:center;gap:12px;border:1px solid #e6e1ec;border-radius:8px;background:#fff;margin-top:18px;padding:14px}.destination-select{display:flex;align-items:center;gap:12px;flex:1;min-width:0;text-align:left}.destination-select>span{min-width:0;flex:1}.destination-select strong,.destination-select>span>small{display:block;font-size:15px;font-weight:400;overflow-wrap:anywhere}.destination-select strong small{font-size:12px;color:#98909e}.destination-select>span>small{margin-top:6px;color:#8d8595}.destination-select>svg{color:#9b88b9;flex-shrink:0}.edit-link{font-size:13px;color:#8b69c0;text-decoration:underline}.account-editor{background:#fff}.account-editor-form{padding:14px 28px 28px}.account-editor-form label{display:flex;align-items:center;gap:12px;min-height:66px;border-bottom:1px solid #e4e4e4;font-size:16px}.account-editor-form label>span{flex-shrink:0;min-width:92px;white-space:nowrap}.account-editor-form label i{color:#c66b7e;font-style:normal;padding-left:3px}.account-editor-form input,.account-editor-form select{flex:1;width:0;min-width:0;background:#fff;border:0;border-radius:0;outline:none;padding:15px 0;font-size:16px;line-height:1.5;color:#333}.account-editor-form input::placeholder{color:#bdbdbd}.account-editor-form label:focus-within{border-bottom-color:#895fe6}.account-editor-form .account-primary{margin-top:30px}.account-message{padding:20px 24px;font-size:14px;line-height:1.7;margin:0}.account-editor-form .account-message{padding:16px 0 0}.account-message button{color:#895cbf;text-decoration:underline}.error{color:#b95e71}.account-hint{font-size:12px;line-height:1.8;color:#92859b;margin:16px 0}.withdrawal-accounts button:disabled,.withdrawal-accounts input:disabled,.withdrawal-accounts select:disabled{opacity:.5;cursor:not-allowed}button:focus-visible{outline:2px solid #9b7de6;outline-offset:3px}@media(max-width:760px){.withdrawal-accounts header{min-height:48px;padding:0 8px}.withdrawal-accounts header h1{font-size:18px}.manager-content{padding:20px}.account-editor-form{padding:12px 22px 24px}.account-editor-form label{min-height:62px;font-size:15px;gap:8px}.account-editor-form label>span{min-width:91px}}@media(max-width:360px){.account-editor-form{padding-left:16px;padding-right:16px}.account-editor-form label>span{min-width:84px;font-size:14px}}
</style>
