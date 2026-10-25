import {api} from './account'
export interface AgentNode {id:string;username:string;name?:string;role:string;status:string;created_at:string;inviter_id?:string;inviter_username?:string;missing_parent:boolean;deposit_cents:number;direct_count:number;team_count:number;team_deposit_cents:number;today_stake_cents:number;team_today_stake_cents:number}
export interface AgentPage {items:AgentNode[];parent?:AgentNode;total:number;page:number;page_size:number;day:string;today_stake_cents:number}
export function loadAgents(query:{username?:string;parent_id?:string;page?:number}={}) {
 const params=new URLSearchParams()
 if(query.username)params.set('username',query.username)
 if(query.parent_id)params.set('parent_id',query.parent_id)
 params.set('page',String(query.page??1))
 return api<AgentPage>('/admin/agents?'+params.toString())
}
