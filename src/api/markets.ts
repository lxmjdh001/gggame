export interface MarkSixSelectionSchema {type:'number'|'zodiac'|'tail';mode:'combination'|'set';pick_count:number;min_value:number;max_value:number;max_picks:number}
export interface MarkSixContext {issue:string;draw_at:string;lunar_year:number;zodiacs:{value:number;label:string;numbers:number[]}[];tails:{value:number;label:string;numbers:number[]}[];numbers:{number:number;color:'red'|'blue'|'green';zodiac:number;tail:number}[]}
export interface PayoutTier {key:string;label:string;default_odds_milli:number}
export type TierOdds = Record<string,number>
export interface Market {id:string;group:string;label:string;category:'two_sides'|'regular';kind:string;positions:number[];pick:string;rule:string;rule_version:number;default_odds_milli:number;ui_group?:string;selection_schema?:MarkSixSelectionSchema;payout_tiers?:PayoutTier[];max_payout_multiplier?:number;
 /** Client-only identity and values for a concrete combination of a quoted template. */
 quote_id?:string;selection_values?:number[];selection_context_issue?:string}
export interface Quote {market_id:string;odds_milli:number;enabled:boolean;tier_odds_milli?:TierOdds;current_zodiac_odds_milli?:number;zero_tail_odds_milli?:number}
export interface MarketBook {session_id:string;markets:Market[];quotes:Quote[];marksix_context?:MarkSixContext}
export const oddsText=(value:number)=>(value/1000).toFixed(3)
export function oddsMilli(value:string){if(!/^\d+(\.\d{1,3})?$/.test(value.trim()))throw new Error('赔率最多保留三位小数');const n=Math.round(Number(value)*1000);if(!Number.isSafeInteger(n)||n<1000||n>1000000)throw new Error('赔率范围为1.000–1000.000');return n}

export interface MarketBatchInput {session_id:string;issue:string;unit_cents:number;amount_cents:number;selection_amounts?:Record<string,number>;selections:{market_id:string;odds_milli:number;tier_odds_milli?:TierOdds;values?:number[]}[];key:string}
