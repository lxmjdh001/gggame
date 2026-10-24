import type {Market,MarkSixContext} from '../api/markets'

export const markSixGroups=[
 {id:'zodiac_tail',label:'一肖/尾数'}, {id:'special',label:'特码'}, {id:'regular',label:'正码'},
 {id:'position_numbers',label:'正特码'}, {id:'position_faces',label:'正特1-6'},
 {id:'half_wave',label:'特码半波'}, {id:'special_zodiac',label:'特码生肖'}, {id:'combined_zodiac',label:'合肖'},
 {id:'miss',label:'全不中'}, {id:'linked_tail',label:'尾数连'}, {id:'linked_zodiac',label:'连肖'}, {id:'sum',label:'总和'},
] as const
// Betting presentation only. Keep server contracts/quotes intact for historical
// orders and admin configuration; do not merge every `sum` market into 特码.
export const markSixBettingGroups=markSixGroups.filter(group=>group.id!=='sum')
const specialBettingKinds=new Set(['number','lhc_size','lhc_parity','lhc_digit_size','lhc_digit_parity','lhc_tail_size','lhc_color'])
export function markSixBettingGroup(market:Market):string|null{
 const group=market.ui_group==='position_number'?'position_numbers':market.ui_group
 if(group==='special'&&!specialBettingKinds.has(market.kind))return null
 if(group==='sum')return market.id.startsWith('lhc6.sum.')&&['lhc_size','lhc_parity'].includes(market.kind)?'regular':null
 if(group==='linked_numbers')return null
 return group??null
}
export const quoteID=(market:Market)=>market.quote_id??market.id
export const canonicalValues=(values:number[])=>[...new Set(values)].sort((a,b)=>a-b)
export const selectionID=(marketID:string,values:number[])=>`${marketID}[${canonicalValues(values).join(',')}]`
export function usesZodiac(market:Market){return !!market.selection_context_issue||market.selection_schema?.type==='zodiac'||['special_zodiac','combined_zodiac','linked_zodiac'].includes(market.ui_group??'')||market.kind.includes('zodiac')||market.ui_group==='zodiac_tail'&&market.group.includes('肖')}
export function combinationCount(n:number,k:number,limit=500){
 if(!Number.isInteger(n)||!Number.isInteger(k)||k<1||n<k)return 0
 let result=1
 for(let i=1;i<=Math.min(k,n-k);i++){result=result*(n-i+1)/i;if(result>limit)return limit+1}
 return Math.round(result)
}
export function expandCombinations(values:number[],count:number,limit=500):number[][]{
 const sorted=canonicalValues(values)
 if(combinationCount(sorted.length,count,limit)>limit)return []
 const out:number[][]=[]
 function visit(start:number,picked:number[]){if(picked.length===count){out.push(picked);return}for(let i=start;i<=sorted.length-(count-picked.length);i++)visit(i+1,[...picked,sorted[i]!])}
 if(count>0&&sorted.length>=count)visit(0,[])
 return out
}
export function markSixValueLabel(market:Market,value:number,context?:MarkSixContext){
 if(market.selection_schema?.type==='zodiac')return context?.zodiacs.find(item=>item.value===value)?.label??String(value)
 if(market.selection_schema?.type==='tail')return context?.tails.find(item=>item.value===value)?.label??`${value}尾`
 return String(value).padStart(2,'0')
}
export function concreteMarket(market:Market,values:number[],context?:MarkSixContext):Market{
 const sorted=canonicalValues(values)
 return {...market,id:selectionID(market.id,sorted),quote_id:market.id,selection_values:sorted,
  label:`${market.label} · ${sorted.map(value=>markSixValueLabel(market,value,context)).join('、')}`,
  ...(usesZodiac(market)&&context?{selection_context_issue:context.issue}:{})}
}
