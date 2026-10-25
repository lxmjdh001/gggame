import {api} from './account'
export interface WithdrawalAddress {version:number;network:'TRC20'|'ERC20';address:string}
export const loadWithdrawalAddresses=()=>api<WithdrawalAddress[]>('/wallet/withdrawal-addresses')
export const saveWithdrawalAddress=(network:string,version:number,address:string)=>api<WithdrawalAddress>(`/wallet/withdrawal-addresses/${encodeURIComponent(network)}`,'PUT',{version,address})
