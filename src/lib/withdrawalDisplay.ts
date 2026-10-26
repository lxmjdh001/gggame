export function maskedName(value?:string){
 const chars=Array.from(value?.trim()??'')
 return chars.length>2?'***'+chars.slice(-2).join(''):chars.length?'*'+chars[chars.length-1]:'未设置'
}
export function maskedDestination(value:string){
 const chars=Array.from(value.trim())
 return chars.length?'*****'+(chars.length>4?chars.slice(-4).join(''):''):'请选择收款账号'
}
export function withdrawalRemark(status:string,note?:string){
 const text={pending:'提款申请处理中',approved:'提款申请已审核通过',rejected:'提款申请已驳回，申请金额已退回'}[status]??'提款申请状态待确认'
 return note?.trim()?text+'；'+note.trim():text
}
