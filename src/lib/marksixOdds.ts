import type {Market,MarkSixContext,Quote} from '../api/markets'

export function supportsCurrentZodiacOdds(m:Market){
 return m.rule_version===6&&(m.kind==='lhc_zodiac'||m.selection_schema?.type==='zodiac')
}
export const currentZodiac=(context?:MarkSixContext)=>context?.numbers.find(item=>item.number===1)?.zodiac
export function selectionOdds(m:Market,q?:Quote,context?:MarkSixContext):number{
 if(!q)return 0
 const current=currentZodiac(context)
 if(q.current_zodiac_odds_milli&&supportsCurrentZodiacOdds(m)&&current!==undefined){
  const values=m.selection_schema?m.selection_values??[]:[Number(m.pick)]
  if(values.includes(current))return q.current_zodiac_odds_milli
 }
 if(q.zero_tail_odds_milli&&m.kind==='lhc_link_tail'&&m.selection_values?.includes(0))return q.zero_tail_odds_milli
 return q.odds_milli
}
export const shortOdds=(milli:number)=>String(milli/1000)
export function conditionalOddsHint(m:Market,q?:Quote){
 if(!q)return ''
 const hints:string[]=[]
 if(q.current_zodiac_odds_milli)hints.push(`${m.selection_schema?'含':'为'}本年生肖 ${shortOdds(q.current_zodiac_odds_milli)}倍`)
 if(q.zero_tail_odds_milli)hints.push(`含0尾 ${shortOdds(q.zero_tail_odds_milli)}倍`)
 return hints.join('；')
}
