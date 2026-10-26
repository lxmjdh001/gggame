import {api,type WalletRequest} from './account'
export interface WithdrawalDetail {request:WalletRequest;balance_before_cents:number|null;balance_after_cents:number|null}
export const loadWithdrawalDetail=(id:string)=>api<WithdrawalDetail>(`/wallet/requests/${encodeURIComponent(id)}/detail`)
export interface WithdrawalAddress {version:number;network:'TRC20'|'ERC20'|'OKPAY'|'TOPAY'|'KDPAY'|'FPAY'|'808';address:string}
export const loadWithdrawalAddresses=()=>api<WithdrawalAddress[]>('/wallet/withdrawal-addresses')
export const saveWithdrawalAddress=(network:string,version:number,address:string)=>api<WithdrawalAddress>(`/wallet/withdrawal-addresses/${encodeURIComponent(network)}`,'PUT',{version,address})
