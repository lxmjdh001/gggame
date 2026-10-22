import {oddsText,type Market,type TierOdds} from '../api/markets'

export const copyTierOdds=(odds?:TierOdds):TierOdds|undefined=>odds?{...odds}:undefined
export function sameTierOdds(a?:TierOdds,b?:TierOdds):boolean{
 const keys=Object.keys(a??{})
 return keys.length===Object.keys(b??{}).length&&keys.every(key=>a![key]===b?.[key])
}
export function validTierOdds(market:Market,odds?:TierOdds):boolean{
 const tiers=market.payout_tiers??[]
 return tiers.length===Object.keys(odds??{}).length&&tiers.every(tier=>Number.isSafeInteger(odds?.[tier.key])&&odds![tier.key]!>=1000&&odds![tier.key]!<=1000000)
}
export const tierLabel=(key:string)=>({hit2:'中二',hit3:'中三',special:'中特'} as Record<string,string>)[key]??key
export function marketOddsText(market:Market,odds:number,tierOdds?:TierOdds,compact=false):string{
 const format=(value:number)=>compact?String(Number(oddsText(value))):oddsText(value)
 return market.payout_tiers?.length?market.payout_tiers.map(tier=>`${tier.label} ${tierOdds?.[tier.key]===undefined?'待配置':format(tierOdds[tier.key]!)}`).join(' / '):format(odds)
}
interface OrderOdds {odds_milli?:number;tier_odds_milli?:TierOdds;payout_tier?:string;payout_multiplier?:number;payout_odds_milli?:number}
export function orderOddsText(order:OrderOdds):string{
 const entries=Object.entries(order.tier_odds_milli??{})
 return entries.length?entries.map(([key,value])=>`${tierLabel(key)} ${oddsText(value)}`).join(' / '):oddsText(order.odds_milli??0)
}
export function orderPayoutText(order:OrderOdds):string{
 const parts:string[]=[]
 if(order.payout_tier)parts.push(`按${tierLabel(order.payout_tier)}结算${order.payout_odds_milli?`，赔率 ${oddsText(order.payout_odds_milli)}`:''}`)
 if(order.payout_multiplier&&order.payout_multiplier>1)parts.push(`按出现次数派彩 ${order.payout_multiplier} 次`)
 return parts.join(' · ')
}
