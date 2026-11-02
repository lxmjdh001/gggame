// Keep decimal inputs as strings and use fixed precision through cent rounding.
export function validUSDTQuantity(value:string):boolean {
 return /^(0|[1-9]\d{0,18})(\.\d{1,6})?$/.test(value)&&/[1-9]/.test(value)
}
export function usdtDepositCents(quantity:string,rate:string):number|null {
 if(!validUSDTQuantity(quantity)||!/^(0|[1-9]\d{0,6})(\.\d{1,6})?$/.test(rate))return null
 const [whole,fraction='']=quantity.split('.'),[rateWhole,rateFraction='']=rate.split('.')
 const micros=BigInt(whole!)*1000000n+BigInt(fraction.padEnd(6,'0'))
 const rateMicros=BigInt(rateWhole!)*1000000n+BigInt(rateFraction.padEnd(6,'0'))
 if(rateMicros<=0n||rateMicros>1000000000000n)return null
 const cents=(micros*rateMicros+5000000000n)/10000000000n
 return cents<=BigInt(Number.MAX_SAFE_INTEGER)?Number(cents):null
}
