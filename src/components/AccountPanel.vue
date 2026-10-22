<script setup lang="ts">
import {ref,computed,onMounted,watch} from 'vue'
import InviteFriends from './InviteFriends.vue'
import AccountRecords from './AccountRecords.vue'
import MyOrders from './MyOrders.vue'
import WalletPanel from './WalletPanel.vue'
import {Wallet,Users,Gift,FileText,ShieldCheck,ArrowUpRight,LogOut,RefreshCw,LockKeyhole,ChevronRight,UserRound,Settings,ArrowDownToLine,ArrowUpFromLine,History,ChartNoAxesCombined} from 'lucide-vue-next'
import {user,account,api,refreshAccount,money} from '../api/account'
const props=defineProps<{page:string;walletMode:'deposit'|'withdraw'}>();const emit=defineEmits<{login:[];navigate:[page:string];notify:[message:string];logout:[];wallet:[mode:'deposit'|'withdraw'];busy:[busy:boolean]}>()
const passwordOpen=ref(false)
const busy=ref(false),error=ref(''),oldPassword=ref(''),newPassword=ref(''),confirmPassword=ref('')
const title=computed(()=>({wallet:'我的钱包',commissions:'邀请好友',records:'投注记录',ledger:'账变记录',transactions:'交易记录',daily:'每日输赢',profile:'个人中心'}[props.page]??'个人中心'))
async function refresh(){try{await refreshAccount();error.value=''}catch(e){error.value=(e as Error).message}}
onMounted(()=>{if(props.page!=='wallet')void refresh()});watch(()=>props.page,()=>{error.value='';if(props.page!=='wallet')void refresh()})
async function action(fn:()=>Promise<void>){if(busy.value)return;busy.value=true;error.value='';try{await fn()}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
async function changePassword(){await action(async()=>{if(newPassword.value!==confirmPassword.value)throw new Error('两次新密码不一致');await api('/auth/password','POST',{old_password:oldPassword.value,new_password:newPassword.value});user.value=null;account.value=null;emit('notify','密码已更新，请重新登录');emit('login')})}
</script>
<template><div class="account-page" :class="{'reference-profile':page==='profile','reference-wallet':page==='wallet','reference-invite':page==='commissions','reference-orders':page==='records'}"><WalletPanel v-if="page==='wallet'" :mode="walletMode" @back="emit('navigate','profile')" @login="emit('login')" @notify="emit('notify',$event)" @busy="emit('busy',$event)"/><MyOrders v-else-if="page==='records'" @back="emit('navigate','profile')" @login="emit('login')"/><template v-else><header v-if="page==='profile'" class="profile-header"><span>我的</span><button aria-label="修改登陆密码" :aria-expanded="passwordOpen" @click="passwordOpen=!passwordOpen"><Settings :size="21"/></button></header><div v-if="page!=='profile'" class="page-heading"><div><p v-if="page!=='commissions'" class="section-eyebrow">JS · MY ACCOUNT</p><h1>{{title}}</h1><p v-if="page!=='commissions'">账户、余额与记录，集中管理。</p></div><button v-if="user" class="secondary-button" @click="refresh"><RefreshCw :size="16"/>刷新</button></div>
 <div v-if="!user" class="account-empty"><div class="auth-brand"><LockKeyhole :size="30"/></div><h2>登录你的金沙账户</h2><p>登录后查看余额、订单和邀请好友。</p><button class="primary-button" @click="emit('login')">登录 / 注册 <ArrowUpRight :size="17"/></button></div>
 <template v-else><div v-if="page!=='profile'" class="account-tabs"><button v-for="t in [{id:'profile',name:'账户',icon:ShieldCheck},{id:'wallet',name:'钱包',icon:Wallet},{id:'records',name:'订单',icon:FileText},{id:'commissions',name:'邀请好友',icon:Users}]" :class="{active:page===t.id}" @click="emit('navigate',t.id)"><component :is="t.icon" :size="17"/>{{t.name}}</button></div><div v-if="error" class="form-error" role="alert">{{error}}</div>
 <template v-if="page==='profile'">
 <section class="profile-summary"><div class="profile-summary-top"><span class="profile-person"><UserRound :size="36"/></span><div><h2>{{user.username}}</h2><small><ShieldCheck :size="14"/>{{user.role==='admin'?'管理员':'会员'}} · 邀请码 {{user.invite_code}}</small></div><button @click="emit('logout')">退出</button></div><div class="profile-funds"><button @click="emit('navigate','wallet')"><strong>{{money(user.balance_cents)}}<small>元</small></strong><span>资金</span></button><button @click="emit('navigate','wallet')"><strong>{{money(user.frozen_cents+(account?.betting_frozen_cents??0))}}<small>元</small></strong><span>积分</span></button></div></section>
 <nav class="profile-quick" aria-label="账户快捷入口"><button @click="emit('wallet','deposit')"><ArrowDownToLine/>存款</button><button @click="emit('wallet','withdraw')"><ArrowUpFromLine/>提款</button><button @click="emit('navigate','commissions')"><Users/>邀请好友</button><button @click="emit('navigate','wallet')"><Wallet/>钱包</button></nav>
 <nav class="profile-services" aria-label="账户服务"><button @click="emit('navigate','promotions')"><Gift/>自助优惠<ChevronRight/></button><button @click="emit('navigate','ledger')"><History/>账变记录<ChevronRight/></button><button @click="emit('navigate','transactions')"><Wallet/>交易记录<ChevronRight/></button><button @click="emit('navigate','daily')"><ChartNoAxesCombined/>每日输赢<ChevronRight/></button><button @click="emit('navigate','records')"><FileText/>投注记录<ChevronRight/></button><button @click="passwordOpen=!passwordOpen" :aria-expanded="passwordOpen"><LockKeyhole/>修改登陆密码<ChevronRight/></button></nav>
 <section v-if="passwordOpen" class="surface profile-password"><h3>修改登陆密码</h3><form class="form-stack" @submit.prevent="changePassword"><label>原密码<input v-model="oldPassword" type="password" required maxlength="128" autocomplete="current-password"/></label><label>新密码<input v-model="newPassword" type="password" required minlength="10" maxlength="128" autocomplete="new-password" placeholder="至少10位"/></label><label>确认新密码<input v-model="confirmPassword" type="password" required minlength="10" maxlength="128" autocomplete="new-password"/></label><small>修改后，所有设备需要重新登录。</small><button class="primary-button" :disabled="busy">保存新密码</button></form></section>
 </template>
 <InviteFriends v-else-if="page==='commissions'" @notify="emit('notify',$event)"/>
 <AccountRecords v-else-if="page==='ledger'||page==='transactions'||page==='daily'" :kind="page" :key="page"/>
 </template></template></div></template>

<style scoped>
.reference-profile{background:#f5f6f8;max-width:960px;margin:0 auto}.profile-header{position:relative;display:flex;align-items:center;justify-content:center;min-height:55px;background:linear-gradient(105deg,#4b5df1,#8c53e5);color:white;font-size:19px}.profile-header>button{position:absolute;right:14px;display:grid;place-items:center;width:35px;height:35px}.profile-summary{margin:16px;padding:22px;background:#fff;border:1px solid #ededf1;border-radius:7px;box-shadow:0 5px 10px #34314d0d}.profile-summary-top{display:flex;align-items:center;gap:14px}.profile-person{width:64px;height:64px;border-radius:50%;display:grid;place-items:center;background:#f0f0f1;color:#b8bdc5}.profile-summary-top>div{min-width:0}.profile-summary-top h2{font-size:20px;font-weight:500;overflow-wrap:anywhere}.profile-summary-top small{display:flex;align-items:center;gap:5px;font-size:11px;color:#8d929f;margin-top:9px}.profile-summary-top small svg{color:#36bc9b}.profile-summary-top>button{margin-left:auto;background:#6957df;color:white;padding:7px 17px;font-size:13px;border-radius:20px;flex-shrink:0}.profile-funds{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));margin-top:25px}.profile-funds button{text-align:center}.profile-funds strong{display:block;color:#28a1ca;font-size:28px;font-weight:500;overflow-wrap:anywhere}.profile-funds small{font-size:17px;margin-left:3px}.profile-funds span{display:block;font-size:13px;margin-top:6px;color:#767780}.profile-funds button+button strong{color:#7eb55a}.profile-quick{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));padding:12px 7px;background:#fff}.profile-quick button{display:flex;flex-direction:column;align-items:center;gap:9px;padding:12px 2px;font-size:13px;color:#535560}.profile-quick svg{width:25px;height:25px;color:#3ab4d9;stroke-width:2}.profile-services{display:flex;flex-direction:column;background:#fff;margin-top:9px}.profile-services button{display:flex;align-items:center;gap:12px;border-bottom:1px solid #eee;padding:20px;font-size:16px;text-align:left;color:#4e515b}.profile-services svg{color:#43aeca;width:21px;height:21px}.profile-services button:nth-child(3n+1)>svg:first-child{color:#925cbb}.profile-services button:nth-child(3n+2)>svg:first-child{color:#529bbd}.profile-services button>svg:last-child{margin-left:auto;color:#adb2b8;width:18px}.profile-password{margin:16px;border-radius:7px}.profile-password .form-stack{margin-top:18px}
@media(max-width:760px){.reference-profile{margin:0 -16px}.profile-header{min-height:49px;font-size:18px}.profile-summary{margin:12px;padding:18px 14px}.profile-person{width:53px;height:53px}.profile-summary-top{gap:10px}.profile-summary-top h2{font-size:17px}.profile-summary-top small{font-size:10px}.profile-summary-top>button{padding:6px 13px;font-size:12px}.profile-funds{margin-top:22px}.profile-funds strong{font-size:23px}.profile-funds small{font-size:14px}.profile-funds span{font-size:12px}.profile-services button{padding:18px 16px;font-size:15px}.profile-quick button{font-size:12px}.profile-password{margin:12px}}
</style>

<style scoped>
.reference-wallet{max-width:960px;margin:0 auto;background:#fff}
</style>

<style scoped>
.reference-invite{max-width:960px;margin:0 auto;background:#fff;border:1px solid #eceaf1;border-radius:8px;overflow:hidden}
.reference-invite .page-heading{margin:0;padding:20px 22px;border-bottom:1px solid #eeedf2;align-items:center}
.reference-invite .page-heading h1{font-size:24px;font-weight:600;letter-spacing:0}
.reference-invite .page-heading .secondary-button{border:0;background:none;padding:8px 0;font-size:16px;color:#686070;box-shadow:none}
.reference-invite .account-tabs{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));margin:0;padding:0;border:0;border-bottom:1px solid #e9e7ee;border-radius:0;background:#fff;gap:0}
.reference-invite .account-tabs button{display:flex;justify-content:center;gap:0;border-radius:0;padding:16px 4px;font-size:17px;white-space:nowrap}
.reference-invite .account-tabs button svg{display:none}
.reference-invite .account-tabs button.active{color:#7353e8;background:#f5f1ff;box-shadow:inset 0 -2px #7353e8}
.reference-invite .form-error{margin:16px}
@media(max-width:760px){.reference-invite{margin:0;border:0;border-radius:0;min-height:calc(100dvh - 100px)}.reference-invite .page-heading{padding:16px}.reference-invite .page-heading h1{font-size:21px}.reference-invite .account-tabs button{font-size:16px;padding:15px 2px}}
</style>

<style scoped>
@media(max-width:360px){.reference-profile{margin:0 -12px}}
</style>

<style scoped>
.reference-orders{max-width:960px;margin:0 auto;background:#fff}
</style>
