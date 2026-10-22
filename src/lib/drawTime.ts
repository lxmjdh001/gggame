// Display only: never use this formatted value for countdowns, cutoffs or settlement.
export function formatDrawTime(value:string, style:'full'|'short'|'time'='full'):string {
 const display=new Date(value)
 if(!Number.isFinite(display.getTime()))return '—'
 display.setUTCSeconds(0,0)
 const fields:Intl.DateTimeFormatOptions=style==='time'
  ? {hour:'2-digit',minute:'2-digit',second:'2-digit'}
  : style==='short'
   ? {month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit'}
   : {}
 return display.toLocaleString(style==='time'?'sv-SE':'zh-CN',{timeZone:'Asia/Shanghai',hour12:false,...fields})
}
