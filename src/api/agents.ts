import {api,APIError,user,account} from './account'
import {withRequestTimeout} from '../lib/requestTimeout'
import {agentExportName,type AgentExportFile,type AgentExportSnapshot} from '../lib/agentExport'
export interface AgentNode {id:string;username:string;name?:string;role:string;status:string;created_at:string;inviter_id?:string;inviter_username?:string;missing_parent:boolean;deposit_cents:number;direct_count:number;team_count:number;team_deposit_cents:number;today_stake_cents:number;team_today_stake_cents:number}
export interface AgentPage {items:AgentNode[];parent?:AgentNode;total:number;page:number;page_size:number;day:string;today_stake_cents:number}
export function loadAgents(query:{username?:string;parent_id?:string;page?:number}={}) {
 const params=new URLSearchParams()
 if(query.username)params.set('username',query.username)
 if(query.parent_id)params.set('parent_id',query.parent_id)
 params.set('page',String(query.page??1))
 return api<AgentPage>('/admin/agents?'+params.toString())
}
export async function exportAgents(username:string,format:'xlsx'|'data',signal:AbortSignal):Promise<AgentExportFile|AgentExportSnapshot> {
 const params=new URLSearchParams({format});if(username)params.set('username',username)
 return withRequestTimeout(async requestSignal=>{
  const response=await fetch('/api/v1/admin/agents/export?'+params.toString(),{credentials:'same-origin',cache:'no-store',signal:requestSignal}).catch(()=>{throw new Error('网络连接异常，请检查网络后重试')})
  if(!response.ok){
   const body=await response.json().catch(()=>null)
   if(response.status===401){user.value=null;account.value=null}
   throw new APIError(body?.error?.message??`导出失败（${response.status}）`,response.status)
  }
  if(format==='data'){
   const body=await response.json().catch(()=>null)
   if(!Array.isArray(body?.data?.items)||typeof body.data.day!=='string')throw new Error('导出数据不完整，请重试')
   return body.data as AgentExportSnapshot
  }
  if(!response.headers.get('Content-Type')?.startsWith('application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'))throw new Error('导出文件异常，请重试')
  const blob=await response.blob();if(!blob.size)throw new Error('导出文件为空，请重试')
  // Filename from the server is ASCII-only and never contains account input.
  const name=response.headers.get('Content-Disposition')?.match(/filename="(agent-relations-[0-9-]+\.xlsx)"/)?.[1]
  return {blob,filename:name??agentExportName('','xlsx')}
 },60000,signal)
}
