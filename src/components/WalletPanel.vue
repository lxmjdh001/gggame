<script setup lang="ts">
import PaymentRequestDetails from './PaymentRequestDetails.vue'
import {computed,onMounted,onUnmounted,ref,watch} from 'vue'
import {ArrowDownToLine,ArrowUpFromLine,Check,ChevronDown,ChevronLeft,Copy,CreditCard,FileText,LockKeyhole,RefreshCw,Wallet} from 'lucide-vue-next'
import {APIError,account,api,cents,datetime,key,labels,money,refreshAccount,user,type User,type WalletRequest,type WithdrawalSettings} from '../api/account'

type WalletKind='deposit'|'withdraw'
interface EnabledPaymentSettings {version:number;enabled:true;network:'TRC20'|'ERC20';address:string;rate_yuan:string;min_amount_cents:number;max_amount_cents:number}
type PaymentSettings=EnabledPaymentSettings|{version:number;enabled:false}
type RequestPayload={kind:WalletKind;amount_cents:number;note:string;transaction_id?:string;payment_method?:'usdt';withdrawal_settings_version?:number;withdrawal_rate_version?:number;payout_network?:string;payout_address?:string;withdrawal_password?:string;withdrawal_password_confirmation?:string;key:string}|{kind:'deposit';payment_method:'usdt';usdt_amount:string;transaction_id:string;settings_version:number;note:string;key:string}
const props=defineProps<{mode:WalletKind}>()
const emit=defineEmits<{back:[];login:[];notify:[message:string];busy:[busy:boolean]}>()
const kind=ref<WalletKind>(props.mode),busy=ref(false),refreshing=ref(false),error=ref('')
const drafts=ref({deposit:{amount:'',note:''},withdraw:{amount:'',note:''}})
const draft=computed(()=>drafts.value[kind.value])
const method=ref<'manual'|'usdt'>('manual'),usdtAmount=ref(''),transactionId=ref(''),manualTransactionId=ref(''),usdtNote=ref(''),withdrawalPassword=ref(''),withdrawalConfirmation=ref('')
const withdrawalSettings=ref<WithdrawalSettings|null>(null),withdrawalError=ref(''),payoutNetwork=ref('TRC20'),payoutAddress=ref('')
const settings=ref<PaymentSettings|null>(null),settingsLoading=ref(false),settingsError=ref('')
const pinSetupOpen=ref(false),pinSetupBusy=ref(false),currentPin=ref(''),newPin=ref(''),confirmPin=ref(''),pinSetupError=ref('')
const pending=ref<{owner:{id:string;username:string};payload:RequestPayload;payment?:EnabledPaymentSettings;withdrawal?:WithdrawalSettings}|null>(null)
const locked=computed(()=>busy.value||pinSetupBusy.value||!!pending.value)
const needsPin=computed(()=>kind.value==='withdraw'&&!!user.value&&!user.value.withdrawal_password_set)
const showPinSetup=computed(()=>kind.value==='withdraw'&&!!user.value&&(needsPin.value||pinSetupOpen.value))
const requestsOpen=ref(false)
const kindLabel=computed(()=>kind.value==='deposit'?'存款':'提款')
const activeWithdrawal=computed(()=>pending.value?.withdrawal??withdrawalSettings.value)
const isUsdtWithdrawal=computed(()=>kind.value==='withdraw'&&method.value==='usdt')
const wagerReady=computed(()=>!!account.value?.wagering?.eligible)
const feePreview=computed(()=>{try{const amount=cents(draft.value.amount);if(amount<100||amount>100000000||!activeWithdrawal.value)return null;const fee=Math.floor((amount*activeWithdrawal.value.fee_bps+5000)/10000);return {fee,net:amount-fee}}catch{return null}})
const usdtPayoutPreview=computed(()=>{if(!feePreview.value||!activeWithdrawal.value)return '—';const [whole,fraction='']=activeWithdrawal.value.rate_yuan.split('.');if(!whole)return '—';const micros=BigInt(whole)*1000000n+BigInt(fraction.padEnd(6,'0'));if(micros<=0n)return '—';const n=BigInt(Math.max(0,feePreview.value.net))*10000000000n/micros;return `${n/1000000n}.${String(n%1000000n).padStart(6,'0')}`})
const isUsdt=computed(()=>kind.value==='deposit'&&method.value==='usdt')
const activePayment=computed(()=>pending.value?.payment??(settings.value?.enabled?settings.value:null))
const usdtAvailable=computed(()=>!!activePayment.value)
const usdtUnavailable=computed(()=>!user.value?'登录后查看 USDT 通道':settingsLoading.value?'正在读取支付配置':settingsError.value||'USDT 通道暂未开放')
const estimatedCents=computed(()=>{
 const rate=activePayment.value?.rate_yuan
 if(!rate||!/^\d+(\.\d{1,6})?$/.test(rate)||!/^\d{1,15}$/.test(usdtAmount.value.trim()))return null
 const [whole,fraction='']=rate.split('.')
 const scaled=BigInt(whole!)*1000000n+BigInt(fraction.padEnd(6,'0'))
 const value=(BigInt(usdtAmount.value.trim())*scaled+5000n)/10000n
 return value<=BigInt(Number.MAX_SAFE_INTEGER)?Number(value):null
})
const pendingNotice=computed(()=>pending.value?user.value?'申请结果待确认，请重试原申请，重复确认不会重复提交。':`申请结果待确认，请重新登录原账号 ${pending.value.owner.username} 后重试。`:'')
const pendingCount=computed(()=>account.value?.requests.filter(r=>r.status==='pending').length??0)
const walletLabel=(value:string)=>({deposit:'存款',withdraw:'提款',withdraw_hold:'提款冻结',withdraw_approved:'提款完成',withdraw_rejected:'提款解冻',deposit_approved:'存款入账'}[value]??labels[value]??value)
let mounted=true
watch(locked,value=>emit('busy',value),{immediate:true,flush:'sync'})
watch(()=>props.mode,value=>{if(!locked.value){kind.value=value;error.value=''}})
function clearDrafts(){drafts.value={deposit:{amount:'',note:''},withdraw:{amount:'',note:''}};usdtAmount.value='';transactionId.value='';manualTransactionId.value='';usdtNote.value='';withdrawalPassword.value='';withdrawalConfirmation.value='';method.value='manual';payoutAddress.value=''}
watch(()=>user.value?.id,(next,previous)=>{
 settings.value=null;settingsError.value='';withdrawalSettings.value=null;withdrawalError.value='';payoutAddress.value=''
 currentPin.value='';newPin.value='';confirmPin.value='';pinSetupError.value='';pinSetupOpen.value=false
 if(pending.value){
  if(!next){emit('notify',pendingNotice.value);return}
  if(next!==pending.value.owner.id){
   const name=pending.value.owner.username
   pending.value=null;clearDrafts()
   error.value=`已切换账号，原申请未向当前账号提交。${name} 的申请结果仍待确认，请登录该账号查看申请记录。`
   emit('notify',error.value)
  }
 }else if(previous&&next!==previous){clearDrafts();error.value=''}
 if(next){void loadSettings();void loadWithdrawalSettings()}
},{flush:'sync'})
function switchKind(next:WalletKind){if(locked.value)return;kind.value=next;method.value='manual';error.value='';withdrawalPassword.value='';withdrawalConfirmation.value='';currentPin.value='';newPin.value='';confirmPin.value='';pinSetupError.value='';pinSetupOpen.value=false}
function switchMethod(next:'manual'|'usdt'){if(locked.value||next==='usdt'&&(kind.value==='deposit'?!usdtAvailable.value:!activeWithdrawal.value?.usdt_enabled))return;method.value=next;error.value=''}
async function loadSettings(){
 const owner=user.value?.id;if(!owner)return
 settingsLoading.value=true
 try{
  const result=await api<PaymentSettings>('/wallet/payment-settings')
  if(mounted&&user.value?.id===owner){
   if(result.enabled){
    const normalized={...result,version:result.version??0,network:result.network??'',address:result.address??'',rate_yuan:result.rate_yuan??'',min_amount_cents:result.min_amount_cents??0,max_amount_cents:result.max_amount_cents??0}
    settings.value=normalized.address&&['TRC20','ERC20'].includes(normalized.network)&&normalized.rate_yuan&&normalized.max_amount_cents>0?normalized:{version:normalized.version,enabled:false}
   }else settings.value={version:result.version??0,enabled:false}
   if(!settings.value.enabled&&method.value==='usdt'&&!pending.value){method.value='manual';error.value='USDT 通道暂未开放，请选择其他存款方式'}
   settingsError.value=''
  }
 }
 catch(e){if(mounted&&user.value?.id===owner){settings.value=null;settingsError.value=(e as Error).message}}
 finally{if(mounted)settingsLoading.value=false}
}
async function loadWithdrawalSettings(){const owner=user.value?.id;if(!owner)return;try{const cfg=await api<WithdrawalSettings>('/wallet/withdrawal-settings');if(mounted&&user.value?.id===owner){withdrawalSettings.value=cfg;withdrawalError.value=''}}catch(e){if(mounted&&user.value?.id===owner){withdrawalSettings.value=null;withdrawalError.value=(e as Error).message}}}
async function refresh(){
 if(refreshing.value||!user.value)return
 refreshing.value=true
 await Promise.all([loadSettings(),loadWithdrawalSettings(),refreshAccount().catch(e=>{error.value=(e as Error).message})])
 refreshing.value=false
}
async function copyAddress(){
 if(!activePayment.value||locked.value)return
 try{await navigator.clipboard.writeText(activePayment.value.address);emit('notify','收款地址已复制')}catch{error.value='无法自动复制，请长按或选中完整收款地址复制'}
}
async function saveWithdrawalPassword(){
 if(locked.value||!user.value)return
 pinSetupError.value=''
 if(!/^\d{6}$/.test(newPin.value)){pinSetupError.value='提现密码须为 6 位数字';return}
 if(newPin.value!==confirmPin.value){pinSetupError.value='两次提现密码不一致';return}
 if(user.value.withdrawal_password_set&&!/^\d{6}$/.test(currentPin.value)){pinSetupError.value='请输入原 6 位数字提现密码';return}
 const owner=user.value.id
 pinSetupBusy.value=true
 try{
  const updated=await api<User>('/auth/withdrawal-password','POST',{current_withdrawal_password:user.value.withdrawal_password_set?currentPin.value:'',withdrawal_password:newPin.value,withdrawal_password_confirmation:confirmPin.value})
  if(user.value?.id!==owner)return
  user.value=updated;if(account.value?.user.id===owner)account.value.user=updated
  currentPin.value='';newPin.value='';confirmPin.value='';withdrawalPassword.value='';withdrawalConfirmation.value='';pinSetupOpen.value=false
  emit('notify','提现密码已保存')
 }catch(e){pinSetupError.value=(e as Error).message;if(e instanceof APIError&&e.status===401)emit('login')}
 finally{pinSetupBusy.value=false}
}
async function submit(){
 if(busy.value||pinSetupBusy.value)return
 if(!user.value){emit('login');return}
 error.value=''
 if(!pending.value){
  try{
   let payload:RequestPayload,payment:EnabledPaymentSettings|undefined,withdrawal:WithdrawalSettings|undefined
   if(isUsdt.value){
    payment=activePayment.value??undefined
    if(!payment)throw new Error('USDT 通道暂不可用，请刷新支付配置')
    if(!/^[1-9]\d{0,14}$/.test(usdtAmount.value.trim()))throw new Error('请输入正整数 USDT 数量')
    if(estimatedCents.value===null||estimatedCents.value<payment.min_amount_cents||estimatedCents.value>payment.max_amount_cents)throw new Error(`存款金额须在 ${money(payment.min_amount_cents)}–${money(payment.max_amount_cents)} 元范围内`)
    const hash=transactionId.value.trim()
    if(!(payment.network==='TRC20'?/^[a-fA-F0-9]{64}$/:/^0x[a-fA-F0-9]{64}$/).test(hash))throw new Error(`请填写 ${payment.network} 网络的完整交易哈希`)
    payload={kind:'deposit',payment_method:'usdt',usdt_amount:usdtAmount.value.trim(),transaction_id:hash,settings_version:payment.version,note:usdtNote.value.trim(),key:key()}
   }else{
    const amount=cents(draft.value.amount),note=draft.value.note.trim()
    if(amount<=0)throw new Error('请输入大于零的金额，最多保留两位小数')
    if(note.length<2)throw new Error(kind.value==='deposit'?'请填写到账凭证或说明':'请填写收款方式与说明')
    if(kind.value==='deposit'&&(!manualTransactionId.value.trim()||new TextEncoder().encode(manualTransactionId.value.trim()).length>128||/[\u0000-\u001f\u007f-\u009f]/.test(manualTransactionId.value)))throw new Error('请填写交易 ID，最多128字节且不能含控制字符')
    if(kind.value==='withdraw'&&amount>user.value.balance_cents)throw new Error('提款金额不能超过当前可用余额')
    if(kind.value==='withdraw'&&!user.value.withdrawal_password_set)throw new Error('请先设置提现密码')
    if(kind.value==='withdraw'&&!/^\d{6}$/.test(withdrawalPassword.value))throw new Error('请输入 6 位数字提现密码')
    if(kind.value==='withdraw'&&withdrawalPassword.value!==withdrawalConfirmation.value)throw new Error('两次提现密码不一致')
    if(kind.value==='withdraw'){
     if(!wagerReady.value)throw new Error('打码量未达标，暂不可提现')
     withdrawal=activeWithdrawal.value??undefined;if(!withdrawal)throw new Error('请刷新提现设置后重试')
     if(isUsdtWithdrawal.value&&!withdrawal.usdt_enabled)throw new Error('USDT提现暂未开放')
     if(isUsdtWithdrawal.value&&!(payoutNetwork.value==='TRC20'?/^T[1-9A-HJ-NP-Za-km-z]{33}$/:/^0x[0-9a-fA-F]{40}$/).test(payoutAddress.value.trim()))throw new Error('请填写与所选网络匹配的完整USDT收款地址')
    }
    payload={kind:kind.value,amount_cents:amount,note,...(withdrawal?{withdrawal_settings_version:withdrawal.version,withdrawal_rate_version:withdrawal.rate_version}:{}),...(isUsdtWithdrawal.value?{payment_method:'usdt' as const,payout_network:payoutNetwork.value,payout_address:payoutAddress.value.trim()}:{}),...(kind.value==='deposit'?{transaction_id:manualTransactionId.value.trim()}:{}),...(kind.value==='withdraw'?{withdrawal_password:withdrawalPassword.value,withdrawal_password_confirmation:withdrawalConfirmation.value}:{}),key:key()}
   }
   pending.value={owner:{id:user.value.id,username:user.value.username},payload,payment,withdrawal}
  }catch(e){error.value=(e as Error).message;return}
 }
 const attempt=pending.value
 if(attempt.owner.id!==user.value.id){error.value='请登录原账号确认这笔申请';return}
 busy.value=true
 try{
  await api<WalletRequest>('/wallet/requests','POST',attempt.payload)
  if(pending.value!==attempt)return
  pending.value=null;drafts.value[attempt.payload.kind]={amount:'',note:''};if(attempt.payload.kind==='deposit')manualTransactionId.value='';else payoutAddress.value='';withdrawalPassword.value='';withdrawalConfirmation.value='';if('payment_method' in attempt.payload){usdtAmount.value='';transactionId.value='';usdtNote.value=''};requestsOpen.value=true
  const message=`${attempt.payload.kind==='deposit'?'存款':'提款'}申请已提交，请等待审核`
  if(user.value?.id!==attempt.owner.id){emit('notify',message);return}
  try{await refreshAccount();emit('notify',message)}catch{error.value='申请已提交，记录刷新失败，请点击刷新查看进度';emit('notify',message)}
 }catch(e){
  if(pending.value!==attempt)return
  if(e instanceof APIError&&[400,404,409,422].includes(e.status)){pending.value=null;error.value=e.message;if(attempt.payload.kind==='withdraw')void refresh();else if('payment_method' in attempt.payload)void loadSettings()}
  else{error.value=pendingNotice.value;emit('notify',pendingNotice.value);if(e instanceof APIError&&e.status===401)emit('login')}
 }finally{busy.value=false}
}
onMounted(()=>void refresh())
onUnmounted(()=>{mounted=false;emit('busy',false)})
</script>

<template>
 <section class="wallet-panel">
  <header class="wallet-header"><button :disabled="locked" aria-label="返回个人中心" @click="emit('back')"><ChevronLeft :size="24"/></button><h1>存 / 提款</h1><button :disabled="refreshing||busy||!user" aria-label="刷新钱包" @click="refresh"><RefreshCw :size="19" :class="{spinning:refreshing}"/></button></header>
  <div class="wallet-content">
   <div class="wallet-mode" role="tablist" aria-label="选择存款或提款"><button role="tab" :aria-selected="kind==='deposit'" :class="{active:kind==='deposit'}" :disabled="locked" @click="switchKind('deposit')">存款</button><button role="tab" :aria-selected="kind==='withdraw'" :class="{active:kind==='withdraw'}" :disabled="locked" @click="switchKind('withdraw')">提款</button></div>
   <div class="wallet-balance"><div><span>可用余额</span><strong>{{user?money(user.balance_cents):'—'}}<small>元</small></strong></div><Wallet :size="25"/></div>
   <section class="wallet-method-section" aria-labelledby="wallet-method-heading"><h2 id="wallet-method-heading">{{kind==='deposit'?'支付方式':'提款方式'}}</h2><div class="wallet-methods"><button class="wallet-method" :class="{selected:method==='manual'}" :aria-pressed="method==='manual'" :disabled="locked" @click="switchMethod('manual')"><component :is="kind==='deposit'?ArrowDownToLine:ArrowUpFromLine" :size="26"/><span>人工{{kindLabel}}</span><Check v-if="method==='manual'" :size="11"/></button><button v-if="kind==='deposit'||activeWithdrawal?.usdt_enabled" class="wallet-method" :class="{selected:method==='usdt'}" :aria-pressed="method==='usdt'" :disabled="locked||(kind==='deposit'?!usdtAvailable:!activeWithdrawal?.usdt_enabled)" @click="switchMethod('usdt')"><CreditCard :size="26"/><span>USDT 账户</span><Check v-if="method==='usdt'" :size="11"/></button><slot name="payment-methods" :kind="kind" :locked="locked"/></div><p v-if="kind==='deposit'&&!usdtAvailable" class="wallet-method-note">{{usdtUnavailable}}<button v-if="user" :disabled="settingsLoading||locked" @click="loadSettings">刷新</button></p></section>
   <section v-if="isUsdt&&activePayment" class="wallet-usdt-details" aria-label="USDT 收款信息"><div class="wallet-address-row"><span>收款地址</span><code>{{activePayment.address}}</code><button :disabled="locked" @click="copyAddress"><Copy :size="12"/>复制</button></div><div class="wallet-payment-row"><span>转账网络</span><strong>{{activePayment.network}}</strong></div><div class="wallet-payment-row"><span>USDT 汇率</span><strong>1 USDT = {{activePayment.rate_yuan}} 元</strong></div><div class="wallet-payment-row"><span>存款金额</span><strong>{{estimatedCents===null?'—':money(estimatedCents)}} 元</strong></div></section>
   <slot name="payment-details" :kind="kind" :locked="locked"/>
   <section v-if="kind==='withdraw'&&user" class="wallet-wagering" aria-label="提现打码进度"><template v-if="account?.wagering"><div><span>打码额度</span><strong>{{money(account.wagering.required_cents)}} 元</strong></div><div><span>打码量</span><strong>{{money(account.wagering.turnover_cents)}} 元</strong></div></template><p v-else>正在读取打码进度，请刷新钱包重试。</p><p v-if="withdrawalError" class="wallet-error">{{withdrawalError}}</p></section>
   <div v-if="kind==='withdraw'&&user" class="wallet-pin-heading"><span>{{needsPin?'提款前请先设置 6 位数字提现密码':'提现密码已设置'}}</span><button v-if="!needsPin" :disabled="locked" :aria-expanded="pinSetupOpen" @click="pinSetupOpen=!pinSetupOpen;pinSetupError=''">{{pinSetupOpen?'收起':'修改提现密码'}}</button></div>
   <form v-if="showPinSetup" class="wallet-pin-setup" @submit.prevent="saveWithdrawalPassword"><h2>{{needsPin?'设置提现密码':'修改提现密码'}}</h2><label v-if="!needsPin" class="wallet-field"><span>原提现密码</span><input v-model="currentPin" :disabled="locked" type="password" inputmode="numeric" required pattern="[0-9]{6}" minlength="6" maxlength="6" autocomplete="off" placeholder="请输入原 6 位数字提现密码"/></label><label class="wallet-field"><span>提现密码</span><input v-model="newPin" :disabled="locked" type="password" inputmode="numeric" required pattern="[0-9]{6}" minlength="6" maxlength="6" autocomplete="new-password" placeholder="请输入 6 位数字"/></label><label class="wallet-field"><span>确认提现密码</span><input v-model="confirmPin" :disabled="locked" type="password" inputmode="numeric" required pattern="[0-9]{6}" minlength="6" maxlength="6" autocomplete="new-password" placeholder="请再次输入提现密码"/></label><p v-if="pinSetupError" class="wallet-error" role="alert">{{pinSetupError}}</p><button class="wallet-submit" :disabled="locked">{{pinSetupBusy?'保存中…':'保存提现密码'}}</button></form>
   <form v-if="!needsPin||pending" class="wallet-form" @submit.prevent="submit">
    <template v-if="isUsdt"><label class="wallet-field"><span>USDT 数量</span><input v-model="usdtAmount" :disabled="locked" inputmode="numeric" required maxlength="15" autocomplete="off" placeholder="请输入 USDT 数量"/></label><label class="wallet-field"><span>交易 ID<small>（必填）</small></span><input v-model="transactionId" :disabled="locked" required maxlength="66" autocomplete="off" spellcheck="false" placeholder="请输入完整区块链交易 ID（交易哈希）"/></label><label class="wallet-field wallet-note-field"><span>转账备注<small>（选填）</small></span><textarea v-model="usdtNote" :disabled="locked" maxlength="500" rows="2" placeholder="请输入转账备注"/></label></template>
    <template v-else><label class="wallet-field"><span>{{kindLabel}}金额<small>（元）</small></span><input v-model="draft.amount" :disabled="locked" inputmode="decimal" required maxlength="20" autocomplete="off" placeholder="请输入金额" aria-label="申请金额（元）"/></label><label v-if="kind==='deposit'" class="wallet-field"><span>交易 ID<small>（必填）</small></span><input v-model="manualTransactionId" :disabled="locked" required maxlength="128" autocomplete="off" spellcheck="false" placeholder="请输入存款交易 ID"/></label><template v-if="isUsdtWithdrawal"><label class="wallet-field"><span>收款网络</span><select v-model="payoutNetwork" :disabled="locked" aria-label="USDT提现收款网络"><option value="TRC20">TRC20</option><option value="ERC20">ERC20</option></select></label><label class="wallet-field"><span>USDT收款地址</span><input v-model="payoutAddress" :disabled="locked" required maxlength="128" autocomplete="off" spellcheck="false" placeholder="请输入所选网络的收款地址"/></label></template><label class="wallet-field wallet-note-field"><span>{{kind==='deposit'?'到账凭证或说明':'收款方式与说明'}}</span><textarea v-model="draft.note" :disabled="locked" required minlength="2" maxlength="500" rows="3" :placeholder="kind==='deposit'?'请填写供管理员核对的到账信息':'请填写收款方式、账号及核对说明'"/></label></template>
    <label v-if="kind==='withdraw'" class="wallet-field"><span>提现密码</span><input v-model="withdrawalPassword" :disabled="locked" type="password" inputmode="numeric" required pattern="[0-9]{6}" minlength="6" maxlength="6" autocomplete="off" placeholder="请输入 6 位数字提现密码"/></label>
    <label v-if="kind==='withdraw'" class="wallet-field"><span>确认提现密码</span><input v-model="withdrawalConfirmation" :disabled="locked" type="password" inputmode="numeric" required pattern="[0-9]{6}" minlength="6" maxlength="6" autocomplete="off" placeholder="请再次输入提现密码"/></label>
    <section v-if="kind==='withdraw'&&activeWithdrawal" class="wallet-withdrawal-quote" aria-label="提现手续费与到账金额"><div><span>提现手续费（{{activeWithdrawal.fee_bps/100}}%）</span><strong>{{feePreview?money(feePreview.fee)+' 元':'—'}}</strong></div><div><span>实际到账</span><strong>{{feePreview?money(feePreview.net)+' 元':'—'}}</strong></div><template v-if="isUsdtWithdrawal"><div><span>USDT汇率</span><strong>1 U＝{{activeWithdrawal.rate_yuan}} 元</strong></div><div><span>预计到账USDT</span><strong>{{usdtPayoutPreview}} U</strong></div><p>手续费从申请金额内扣，USDT保留6位小数。</p></template></section>
    <p v-if="pendingNotice" class="wallet-pending" role="status">{{pendingNotice}}</p>
    <p v-if="error&&error!==pendingNotice" class="wallet-error" role="alert">{{error}}</p>
    <button class="wallet-submit" :disabled="busy||pinSetupBusy||kind==='withdraw'&&!!user&&!pending&&(!wagerReady||!activeWithdrawal)" :type="user||pending?'submit':'button'" @click="!user&&!pending&&emit('login')">{{busy?'提交中…':pending?user?'重试确认':'登录并确认原申请':kind==='deposit'?'存款':!user?'登录后'+kindLabel:'提交'+kindLabel+'申请'}}</button>
   </form>
   <section class="wallet-instructions"><h2>操作说明</h2><p v-if="isUsdt">请使用页面显示的网络及收款地址完成转账，填写整数 USDT 数量和完整交易哈希。提交后由管理员核对，审核通过后按本次申请保存的汇率计入余额。</p><p v-else-if="kind==='deposit'">填写存款金额、交易 ID 及到账凭证或说明，提交后由管理员核对。审核通过后计入可用余额。</p><p v-else>打码额度为充值金额加奖励，下注金额计入打码量，已撤销的下注不计入。打码达标或开通绿色通道后可申请提现；手续费从申请金额内扣。审核通过后重新累计，审核期间新增记录计入下一轮；驳回时退回完整申请金额。</p><p>申请进度和审核说明可在下方记录中查看。</p></section>
   <template v-if="user">
    <section class="wallet-record-section"><button class="wallet-record-toggle" :aria-expanded="requestsOpen" aria-controls="wallet-requests" @click="requestsOpen=!requestsOpen"><FileText :size="18"/><span>存款 / 提款记录<small>{{pendingCount?`${pendingCount} 笔待处理`:'最近 200 条'}}</small></span><ChevronDown :size="17" :class="{expanded:requestsOpen}"/></button><div v-if="requestsOpen" id="wallet-requests" class="wallet-requests"><article v-for="request in account?.requests" :key="request.id"><div><strong>{{walletLabel(request.kind)}}<b>{{money(request.amount_cents)}} 元</b></strong><small>{{datetime(request.created_at)}}</small><p v-if="request.payment_method==='usdt'&&!request.withdrawal" class="wallet-request-usdt">USDT {{request.usdt_amount}} · {{request.payment_network}} · 汇率 {{request.rate_yuan}}<span>交易 ID：{{request.transaction_id}}</span><span>收款地址：{{request.payment_address}}</span></p><p v-if="request.payment_method!=='usdt'&&request.transaction_id" class="wallet-request-usdt">交易 ID：{{request.transaction_id}}</p><PaymentRequestDetails v-if="request.withdrawal" :request="request"/><p>{{request.review_note||request.note}}</p></div><span class="badge" :class="request.status">{{labels[request.status]||request.status}}</span></article><p v-if="!account?.requests.length" class="wallet-empty">暂无申请记录</p></div></section>

   </template>
   <p v-else class="wallet-login-note"><LockKeyhole :size="13"/>登录后可查看申请记录</p>
  </div>
 </section>
</template>

<style scoped>
.wallet-panel{color:#44454f;background:#fff;min-height:100%;padding-bottom:24px}.wallet-header{display:grid;grid-template-columns:42px minmax(0,1fr) 42px;align-items:center;min-height:56px;padding:0 12px;background:linear-gradient(110deg,#4f66ee,#9258ec);color:#fff}.wallet-header h1{margin:0;text-align:center;font-size:20px;font-weight:500}.wallet-header button{display:grid;place-items:center;width:36px;height:38px}.wallet-header button:last-child{justify-self:end}.wallet-content{max-width:760px;margin:0 auto;padding:22px 32px}.wallet-mode{display:grid;grid-template-columns:1fr 1fr;max-width:570px;margin:0 auto 25px;background:#f4f4f6;border-radius:3px;overflow:hidden}.wallet-mode button{padding:13px 10px;font-size:16px;color:#55545d}.wallet-mode button.active{color:#fff;background:linear-gradient(100deg,#46b7e2,#6459ee)}.wallet-balance{display:flex;align-items:center;gap:18px;padding:0 0 22px;border-bottom:1px solid #efedf4;margin-bottom:24px}.wallet-balance>div{flex:1}.wallet-balance>div>span{display:block;font-size:12px;color:#93919d}.wallet-balance strong{display:block;margin-top:6px;font-size:27px;font-weight:500;line-height:1.3;color:#7360c7;font-variant-numeric:tabular-nums;overflow-wrap:anywhere}.wallet-balance strong small{margin-left:7px;font-size:12px;color:#97939e}.wallet-balance>svg{color:#b1a4d8}.wallet-balance dl{display:grid;gap:8px;margin:0;min-width:175px;border-left:1px solid #ece8f3;padding-left:20px}.wallet-balance dl>div{display:flex;gap:14px;justify-content:space-between;font-size:11px}.wallet-balance dt{color:#a09aa9}.wallet-balance dd{margin:0;color:#776f85;font-variant-numeric:tabular-nums}.wallet-method-section h2,.wallet-instructions h2{font-size:16px;font-weight:500;margin:0 0 15px}.wallet-methods{display:flex;flex-wrap:wrap;gap:12px;margin-bottom:27px}.wallet-method{position:relative;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:5px;min-width:120px;min-height:77px;padding:11px 18px;border:1px solid #dedde5;border-radius:4px;color:#8f899c;font-size:12px;background:#fff;overflow:hidden}.wallet-method.selected{border-color:#8a75c2;box-shadow:inset 0 0 0 1px #8a75c220}.wallet-method.selected:after{content:'';position:absolute;bottom:0;right:0;width:0;height:0;border-left:21px solid transparent;border-bottom:20px solid #8270d9}.wallet-method>svg:first-child{color:#a18ccd}.wallet-method>svg:last-child{position:absolute;right:1px;bottom:1px;color:#fff;z-index:1}.wallet-form{margin:0}.wallet-field{display:flex;align-items:center;gap:20px;min-height:56px;margin-bottom:12px;font-size:14px}.wallet-field>span{min-width:128px;flex-shrink:0;color:#55535c}.wallet-field small{font-size:11px;color:#99939e}.wallet-field input,.wallet-field textarea{flex:1;min-width:0;width:100%;background:#fff;border:0;border-bottom:1px solid #e5e2eb;border-radius:0;padding:13px 1px;outline:none;color:#544d61;font-size:14px;line-height:1.6}.wallet-field input:focus,.wallet-field textarea:focus{border-bottom-color:#8d6fdb}.wallet-field input::placeholder,.wallet-field textarea::placeholder{color:#c1bdc6;font-weight:400}.wallet-field textarea{resize:vertical;min-height:80px}.wallet-note-field{align-items:flex-start}.wallet-note-field>span{padding-top:13px}.wallet-submit{display:block;width:100%;min-height:48px;margin:26px 0 25px;padding:12px 20px;border-radius:30px;background:linear-gradient(110deg,#8263ed,#8c5de3);color:#fff;font-size:16px;font-weight:400;letter-spacing:.5px}.wallet-instructions{padding:0 8px 25px}.wallet-instructions h2{font-size:15px;margin-bottom:10px}.wallet-instructions p{font-size:12px;line-height:1.9;color:#9e8ca9;margin:7px 0}.wallet-instructions p:first-of-type{color:#b37c84}.wallet-pending,.wallet-error{font-size:12px;line-height:1.8;padding:10px 12px;border-radius:5px;margin:15px 0 0;background:#fff6ed;color:#b47b54;overflow-wrap:anywhere}.wallet-error{background:#fff1f2;color:#ba6470}.wallet-record-section{border-top:1px solid #eeebf3}.wallet-record-toggle{display:flex;align-items:center;gap:10px;width:100%;padding:19px 2px;text-align:left;color:#7e7291}.wallet-record-toggle>span{flex:1;display:flex;align-items:center;justify-content:space-between;gap:10px;font-size:13px;color:#625a6e}.wallet-record-toggle small{font-size:10px;color:#aca3b6;font-weight:400}.wallet-record-toggle>svg:last-child{color:#aea4bb;transition:transform .2s}.wallet-record-toggle>svg.expanded{transform:rotate(180deg)}.wallet-requests,.wallet-ledger{padding:0 5px 10px;max-height:460px;overflow-y:auto;overscroll-behavior:contain}.wallet-requests article{display:flex;align-items:flex-start;gap:12px;justify-content:space-between;padding:16px 0;border-top:1px solid #f0edf5}.wallet-requests article>div{flex:1;min-width:0}.wallet-requests strong{display:flex;align-items:center;gap:16px;font-size:12px;font-weight:500}.wallet-requests strong b{font-size:13px;font-weight:500;color:#8361af}.wallet-requests small,.wallet-ledger>article>small{display:block;font-size:10px;color:#aaa1b4;margin-top:7px}.wallet-requests p,.wallet-ledger>article>p{font-size:11px;color:#998ba5;line-height:1.8;margin-top:7px;overflow-wrap:anywhere;white-space:pre-wrap}.wallet-requests .badge{flex-shrink:0;font-size:9px!important;padding:4px 7px}.wallet-empty{padding:26px 10px;text-align:center;font-size:12px;color:#aaa0b4}.wallet-ledger article{padding:16px 0;border-top:1px solid #f0edf5}.wallet-ledger-title{display:flex;align-items:center;justify-content:space-between;gap:10px;font-size:12px}.wallet-ledger-title strong{font-weight:500;color:#766580}.wallet-ledger-title b{font-weight:500;color:#a5788c}.wallet-ledger dl{display:flex;gap:22px;margin:10px 0 0;font-size:10px;color:#aaa0b4}.wallet-ledger dl>div{display:flex;gap:7px}.wallet-ledger dd{margin:0;color:#8e7d9c}.wallet-login-note{display:flex;align-items:center;justify-content:center;gap:6px;color:#ada3b5;font-size:11px;padding:15px 0}.wallet-panel button:disabled,.wallet-panel input:disabled,.wallet-panel textarea:disabled{opacity:.5;cursor:not-allowed}.spinning{animation:wallet-spin 1s linear infinite}@keyframes wallet-spin{to{transform:rotate(360deg)}}
@media(max-width:760px){.wallet-panel{min-height:100dvh;padding-bottom:calc(18px + env(safe-area-inset-bottom,0px))}.wallet-header{min-height:48px;padding:0 8px}.wallet-header h1{font-size:18px}.wallet-content{padding:17px 20px}.wallet-mode{margin-bottom:21px}.wallet-mode button{padding:10px;font-size:14px}.wallet-balance{flex-wrap:wrap;gap:10px;padding-bottom:18px;margin-bottom:22px}.wallet-balance strong{font-size:25px}.wallet-balance dl{display:flex;justify-content:space-between;flex-basis:100%;min-width:0;border:0;padding:3px 0 0;gap:14px}.wallet-balance dl>div{gap:8px;font-size:10px}.wallet-balance>svg{width:24px}.wallet-method-section h2{font-size:15px;margin-bottom:12px}.wallet-methods{margin-bottom:22px}.wallet-method{min-width:110px;min-height:73px;font-size:11px}.wallet-field{gap:12px;min-height:49px;font-size:12px;margin-bottom:10px}.wallet-field>span{min-width:105px}.wallet-field small{font-size:10px}.wallet-field input,.wallet-field textarea{font-size:13px;padding-top:12px;padding-bottom:12px}.wallet-note-field>span{padding-top:13px}.wallet-submit{min-height:45px;margin:23px 0;font-size:15px}.wallet-instructions{padding:0 5px 22px}.wallet-instructions h2{font-size:14px}.wallet-instructions p{font-size:11px}.wallet-record-toggle{padding:17px 0}.wallet-record-toggle>span{font-size:12px}.wallet-record-toggle small{font-size:9px}.wallet-ledger dl{gap:12px}.wallet-requests,.wallet-ledger{max-height:55dvh}}
@media(max-width:350px){.wallet-content{padding:16px 15px}.wallet-field{gap:8px}.wallet-field>span{min-width:97px;font-size:11px}.wallet-field input,.wallet-field textarea{font-size:12px}.wallet-balance dl{gap:8px}.wallet-balance dl>div{gap:5px}.wallet-record-toggle>span{gap:6px}.wallet-record-toggle small{font-size:8px}}
@media(prefers-reduced-motion:reduce){.spinning{animation:none}.wallet-record-toggle>svg:last-child{transition:none}}
</style>

<style scoped>
.wallet-method-note{margin:-14px 0 23px;color:#a69bb0;font-size:11px;line-height:1.8}.wallet-method-note button{margin-left:8px;color:#8a6bd0;text-decoration:underline}.wallet-usdt-details{margin:0 0 14px}.wallet-address-row{display:flex;align-items:flex-start;gap:12px;margin:0 0 22px;font-size:13px;line-height:1.8}.wallet-address-row>span{flex-shrink:0;width:85px}.wallet-address-row code{font:inherit;overflow-wrap:anywhere;flex:1;min-width:0;color:#5c536b;user-select:all}.wallet-address-row button{display:flex;align-items:center;gap:4px;flex-shrink:0;border-radius:20px;padding:3px 12px;background:#8d6bdc;color:#fff;font-size:11px}.wallet-payment-row{display:flex;gap:12px;font-size:13px;line-height:1.8;margin:17px 0}.wallet-payment-row>span{width:85px;flex-shrink:0}.wallet-payment-row strong{font-weight:400;color:#665b78}.wallet-usdt-details>p{font-size:11px;color:#b97a82;line-height:1.8;margin:20px 0 0}.wallet-request-usdt span{display:block;overflow-wrap:anywhere}
@media(max-width:760px){.wallet-address-row{gap:8px;font-size:12px;margin-bottom:20px}.wallet-address-row>span,.wallet-payment-row>span{width:72px}.wallet-address-row button{padding:3px 10px;font-size:10px}.wallet-address-row button svg{display:none}.wallet-payment-row{gap:8px;font-size:12px;margin:16px 0}.wallet-usdt-details>p{font-size:10px}.wallet-method-note{font-size:10px}}
</style>

<style scoped>
.wallet-pin-heading{display:flex;align-items:center;justify-content:space-between;gap:10px;margin:0 0 16px;font-size:11px;color:#a494b1;line-height:1.8}.wallet-pin-heading button{color:#8a6bc6;text-decoration:underline;white-space:nowrap}.wallet-pin-setup{padding:18px 16px;margin:0 0 22px;border:1px solid #ebe5f4;border-radius:7px;background:#fdfbff}.wallet-pin-setup h2{font-size:14px;font-weight:500;color:#7e688f;margin:0 0 12px}.wallet-pin-setup input{background:transparent}.wallet-pin-setup .wallet-submit{margin:18px 0 0;font-size:14px;min-height:42px}
@media(max-width:760px){.wallet-pin-setup{padding:14px 11px}.wallet-pin-setup .wallet-field{gap:8px}.wallet-pin-setup .wallet-field>span{min-width:93px;font-size:11px}.wallet-pin-setup .wallet-field input{font-size:12px}.wallet-pin-heading{font-size:10px}}
</style>

<style scoped>
.wallet-wagering,.wallet-withdrawal-quote{padding:14px 12px;margin:0 0 20px;background:#faf8ff;border:1px solid #ece5f5;border-radius:5px;font-size:12px;line-height:1.8}.wallet-wagering>div,.wallet-withdrawal-quote>div{display:flex;justify-content:space-between;gap:12px;margin:5px 0}.wallet-wagering strong,.wallet-withdrawal-quote strong{font-weight:500;font-variant-numeric:tabular-nums;overflow-wrap:anywhere}.wallet-wagering p,.wallet-withdrawal-quote p{font-size:11px;color:#aa7685;margin:5px 0}.wallet-wagering .eligible{color:#438d61}.wallet-field select{flex:1;min-width:0;padding:12px 5px;border:1px solid #e5e2eb;background:#fff;color:#544d61;font-size:13px}.wallet-withdrawal-quote{margin-top:18px}
</style>
