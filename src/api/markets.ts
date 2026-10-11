export interface Market {id:string;group:string;label:string;category:'two_sides'|'regular';kind:string;positions:number[];pick:string;rule:string;rule_version:number;default_odds_milli:number}
export interface Quote {market_id:string;odds_milli:number;enabled:boolean}
export interface MarketBook {session_id:string;markets:Market[];quotes:Quote[]}
export const oddsText=(value:number)=>(value/1000).toFixed(3)
export function oddsMilli(value:string){if(!/^\d+(\.\d{1,3})?$/.test(value.trim()))throw new Error('赔率最多保留三位小数');const n=Math.round(Number(value)*1000);if(!Number.isSafeInteger(n)||n<1000||n>1000000)throw new Error('赔率范围为1.000–1000.000');return n}
