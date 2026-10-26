export const withdrawalAccountTypes = [
 {value:'usdt',label:'USDT'}, {value:'okpay',label:'OKPAY'},
 {value:'topay',label:'TOPAY'}, {value:'kdpay',label:'KDPAY'},
 {value:'fpay',label:'FPAY'}, {value:'808',label:'808钱包'},
] as const
export type WithdrawalAccountType = typeof withdrawalAccountTypes[number]['value']
export const withdrawalAccountLabel = (method?:string) => withdrawalAccountTypes.find(x=>x.value===method)?.label ?? '收款账户'
export const withdrawalMethodForNetwork = (network:string):WithdrawalAccountType => network==='TRC20'||network==='ERC20'?'usdt':withdrawalAccountTypes.find(x=>x.value.toUpperCase()===network)?.value??'usdt'
export function validWithdrawalAccount(network:string,address:string){
 if(network==='TRC20')return /^T[1-9A-HJ-NP-Za-km-z]{33}$/.test(address)
 if(network==='ERC20')return /^0x[0-9a-fA-F]{40}$/.test(address)
 return withdrawalAccountTypes.some(x=>x.value!=='usdt'&&x.value.toUpperCase()===network)&&Array.from(address).length>=1&&Array.from(address).length<=128&&!/[\s\u0000-\u001f\u007f-\u009f]/.test(address)
}
