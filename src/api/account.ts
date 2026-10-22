import { ref, computed } from 'vue'
import type { Session } from './games'
export interface User { id:string;username:string;phone?:string;name?:string;wechat?:string;withdrawal_password_set?:boolean;security_question_set?:boolean;role:string;status:string;invite_code:string;inviter_id?:string;balance_cents:number;frozen_cents:number;created_at:string }
export interface PlaySetting {session_id:string;enabled:boolean;unit_cents:number;max_multiplier:number;close_seconds:number;max_stake_cents:number;user_issue_limit_cents:number}
export interface SiteSettings {registration:boolean;invite_required:boolean;registration_bonus_cents:number;commission_bps:[number,number,number];announcement:string;plays:PlaySetting[]}
export interface Ledger {id:string;kind:string;amount_cents:number;balance_cents:number;frozen_cents:number;note:string;created_at:string}
export interface WithdrawalSettings {version:number;fee_bps:number;usdt_enabled:boolean;rate_yuan:string;rate_version:number}
export interface WithdrawalTerms {version:number;fee_bps:number;fee_cents:number;net_cents:number;rate_yuan?:string;rate_version?:number;usdt_amount?:string;network?:string;address?:string}
export interface Wagering {version:number;revision:string;deposit_cents:number;reward_cents:number;required_cents:number;turnover_cents:number;remaining_cents:number;green_channel:boolean;eligible:boolean}
export interface WalletRequest {withdrawal?:WithdrawalTerms;payment_method?:string;usdt_amount?:string;transaction_id?:string;settings_version?:number;payment_network?:string;payment_address?:string;rate_yuan?:string;id:string;user_id:string;username:string;kind:string;amount_cents:number;status:string;note:string;review_note:string;created_at:string}
export interface Order {market_id?:string;market_name?:string;odds_milli?:number;tier_odds_milli?:Record<string,number>;payout_tier?:string;payout_multiplier?:number;payout_odds_milli?:number;outcome?:string;result_numbers?:number[];id:string;username:string;user_id:string;session_id:string;session_name:string;game_name:string;issue:string;numbers:number[];multiplier:number;cost_cents:number;payout_cents:number;status:string;note:string;created_at:string}
export interface Commission {id:string;level:number;user_id:string;from_username:string;order_id:string;valid_stake_cents:number;rate_bps:number;amount_cents:number;status:string;created_at:string}
export interface Account {wagering?:Wagering;betting_frozen_cents:number;user:User;inviter?:{id:string;username:string};ledger:Ledger[];requests:WalletRequest[];orders:Order[];commissions:Commission[];commission_available_cents:number;commission_total_cents:number;team_counts:number[];members:{id:string;username:string;level:number;created_at:string;balance_cents:number;today_stake_cents:number}[]}
export const user=ref<User|null>(null), account=ref<Account|null>(null), site=ref<SiteSettings|null>(null), authReady=ref(false)
export const balance=computed(()=> (user.value?.balance_cents??0)/100)
export class APIError extends Error {constructor(message:string,public status:number){super(message)}}
export async function api<T>(path:string,method='GET',body?:unknown):Promise<T>{
 const res=await fetch('/api/v1'+path,{method,credentials:'same-origin',cache:'no-store',headers:{'Content-Type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)}),signal:AbortSignal.timeout(12000)}).catch((e:unknown)=>{throw new Error(e instanceof Error&&e.name==='TimeoutError'?'请求超时，请稍后重试':'网络连接异常，请检查网络后重试')})
 const data=await res.json().catch(()=>{throw new Error('服务暂不可用，请稍后重试')});if(!res.ok){if(res.status===401||(res.status===403&&['/account','/auth/me'].includes(path))){user.value=null;account.value=null};throw new APIError(data?.error?.message??`请求失败（${res.status}）`,res.status)};if(!data||typeof data!=='object'||!('data' in data))throw new Error('服务暂不可用，请稍后重试');return data.data as T
}
export async function refreshAccount(){const id=user.value?.id;if(!id)return;const data=await api<Account>('/account');if(user.value?.id===id){account.value=data;user.value=data.user}}
export async function refreshSite(){site.value=await api<SiteSettings>('/site')}
export async function initializeAccount(){await refreshSite();try{user.value=await api<User>('/auth/me');await refreshAccount()}catch(e){if(!(e instanceof APIError&&e.status===401))throw e}finally{authReady.value=true}}
export async function logout(){await api('/auth/logout','POST',{});user.value=null;account.value=null}
export const money=(cents:number=0)=>(cents/100).toLocaleString('en-US',{minimumFractionDigits:2,maximumFractionDigits:2})
export const datetime=(s:string)=>s?new Date(s).toLocaleString('zh-CN',{hour12:false,timeZone:'Asia/Shanghai'}):'—'
export const key=()=>Array.from(crypto.getRandomValues(new Uint8Array(24)),n=>n.toString(16).padStart(2,'0')).join('')
export function cents(s:string):number{if(!/^-?\d+(\.\d{1,2})?$/.test(s.trim()))throw new Error('金额最多保留两位小数');const n=Math.round(Number(s)*100);if(!Number.isSafeInteger(n))throw new Error('金额超出范围');return n}
export const labels:Record<string,string>={pending:'待处理',settled:'已结算',refunded:'已撤销',approved:'已通过',rejected:'已驳回',available:'待领取',claimed:'已领取',active:'正常',disabled:'已停用',deposit:'充值',withdraw:'提现',registration_bonus:'注册赠送',admin_adjust:'管理调账',withdraw_hold:'提现冻结',withdraw_approved:'提现完成',withdraw_rejected:'提现解冻',deposit_approved:'充值入账',order:'投注冻结',order_settled:'订单派彩',order_refunded:'订单退回',commission:'佣金入账'}
export const sessionNames=ref<Record<string,string>>({})
export const sessionCatalog=ref<Session[]>([])
export async function loadSessionNames(){const d=await api<{sessions:Session[]}>('/games');sessionCatalog.value=d.sessions;sessionNames.value=Object.fromEntries(d.sessions.map(s=>[s.id,s.game_name+' · '+s.name]))}
