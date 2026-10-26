import type {AgentNode} from '../api/agents'

type AgentExportNode=Omit<AgentNode,'name'|'role'|'created_at'>&{deleted?:boolean}
export interface AgentExportSnapshot {items:AgentExportNode[];day:string;today_stake_cents:number;exported_at:string;scope:string}
export interface AgentExportFile {blob:Blob;filename:string;note?:string}
const escapeXML=(value:string)=>value.replace(/[&<>"']/g,char=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&apos;'}[char]!))
const amount=(cents:number)=>(cents/100).toFixed(2)+' 元'
export function agentExportName(day:string,extension:string):string {
 return '代理关系-'+day.replace(/[^0-9-]/g,'')+'.'+extension
}

// Draw the complete snapshot, independent of the UI's collapsed or paged nodes.
// Iterative layout also supports invitation chains deeper than the call stack.
export function agentRelationSVG(snapshot:AgentExportSnapshot):{svg:string;width:number;height:number} {
 const nodes=new Map<string,AgentExportNode>(),children=new Map<string,string[]>(),roots:string[]=[]
 for(const node of snapshot.items){if(nodes.has(node.id))throw new Error('代理数据重复，请刷新后重试');nodes.set(node.id,node);children.set(node.id,[])}
 for(const node of snapshot.items){if(node.inviter_id&&nodes.has(node.inviter_id))children.get(node.inviter_id)!.push(node.id);else roots.push(node.id)}
 const cardWidth=310,cardHeight=136,gapX=40,stepY=156,header=132,padding=24
 const positions=new Map<string,{x:number;y:number}>(),stack=roots.map(id=>({id,depth:0,exit:false})).reverse()
 let leaf=0,maxDepth=0
 while(stack.length){
  const entry=stack.pop()!,branch=children.get(entry.id)!
  if(!entry.exit&&branch.length){stack.push({...entry,exit:true});for(let i=branch.length-1;i>=0;i--)stack.push({id:branch[i]!,depth:entry.depth+1,exit:false});continue}
  const y=branch.length?(positions.get(branch[0]!)!.y+positions.get(branch[branch.length-1]!)!.y)/2:header+padding+leaf++*stepY
  positions.set(entry.id,{x:padding+entry.depth*(cardWidth+gapX),y});maxDepth=Math.max(maxDepth,entry.depth)
 }
 if(positions.size!==nodes.size)throw new Error('代理邀请关系存在循环，请刷新后核对')
 const width=Math.max(780,padding*2+(maxDepth+1)*cardWidth+maxDepth*gapX),height=Math.max(260,header+padding*2+Math.max(0,leaf-1)*stepY+cardHeight)
 const parts=[`<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" role="img" aria-labelledby="title desc"><title id="title">代理邀请关系图</title><desc id="desc">${escapeXML(snapshot.scope||'全部团队')}，共${nodes.size}个账号，包含全部层级</desc><rect width="100%" height="100%" fill="#fbf8ff"/><g font-family="Arial, Microsoft YaHei, sans-serif"><text x="24" y="36" font-size="22" font-weight="700" fill="#51405d">代理关系 · ${escapeXML(snapshot.scope||'全部团队')}</text><text x="24" y="62" font-size="12" fill="#786885">共 ${nodes.size} 个账号 · 包含全部层级 · 统计日期 ${escapeXML(snapshot.day)}（北京时间）</text><text x="24" y="86" font-size="13" fill="#7853b7">团队今日流水（含本人）：${amount(snapshot.today_stake_cents)}</text><text x="24" y="108" font-size="11" fill="#786885">导出时间：${escapeXML(snapshot.exported_at)} · 充值含已审核及后台充值，不含奖励；下级统计不含本人</text>`]
 for(const node of snapshot.items){
  const point=positions.get(node.id)!,parent=node.inviter_id?positions.get(node.inviter_id):undefined
  if(parent){const x=parent.x+cardWidth,y=parent.y+cardHeight/2,middle=x+gapX/2;parts.push(`<path d="M${x} ${y}H${middle}V${point.y+cardHeight/2}H${point.x}" fill="none" stroke="#c8b5df" stroke-width="1.2"/>`)}
 }
 for(const node of snapshot.items){
  const {x,y}=positions.get(node.id)!
  const deleted=node.deleted||node.status==='deleted',inactive=deleted||node.status==='disabled'
  const values=[['用户名',node.username+(deleted?'（已删除）':'')],['上级',node.inviter_username||(node.missing_parent?'记录缺失':'无上级')],['个人累计充值',amount(node.deposit_cents)],['直属 / 全部下级',`${node.direct_count} / ${node.team_count} 人`],['下级累计充值',amount(node.team_deposit_cents)]]
  parts.push(`<g transform="translate(${x} ${y})"><rect width="${cardWidth}" height="${cardHeight}" rx="6" fill="${inactive?'#f3f3f3':'#fff'}" stroke="#dfd5f1"/><path d="M1 6V130" stroke="${inactive?'#aaa':'#8660cc'}" stroke-width="3"/>`)
  values.forEach(([label,value],i)=>{const fit=i<2&&value!.length>24?' textLength="182" lengthAdjust="spacingAndGlyphs"':'';parts.push(`<text x="13" y="${25+i*23}" font-size="12" fill="#94859e">${label}</text><text x="297" y="${25+i*23}" text-anchor="end" font-size="12" fill="#51405d"${i===0?' font-weight="600"':''}${fit}>${escapeXML(value!)}</text>`)})
  parts.push('</g>')
 }
 if(!nodes.size)parts.push('<text x="24" y="180" font-size="16" fill="#786885">暂无代理邀请关系</text>')
 parts.push('</g></svg>')
 return {svg:parts.join(''),width,height}
}

export async function agentRelationImage(snapshot:AgentExportSnapshot,signal:AbortSignal):Promise<AgentExportFile> {
 if(signal.aborted)throw new Error('已取消导出')
 const {svg,width,height}=agentRelationSVG(snapshot),vector=new Blob([svg],{type:'image/svg+xml;charset=utf-8'})
 const fallback={blob:vector,filename:agentExportName(snapshot.day,'svg'),note:'已导出完整 SVG 图片，可用浏览器打开并缩放查看。'}
 // Avoid browser canvas limits and large allocations; SVG retains every node.
 if(width>8192||height>8192||width*height>16_000_000)return fallback
 const url=URL.createObjectURL(vector),image=new Image(),canvas=document.createElement('canvas')
 let timer:ReturnType<typeof setTimeout>|undefined,abort:(()=>void)|undefined
 try{
  const blob=await new Promise<Blob>((resolve,reject)=>{
   abort=()=>reject(new Error('已取消导出'));signal.addEventListener('abort',abort,{once:true})
   timer=setTimeout(()=>reject(new Error('图片转换超时')),12000)
   image.onload=()=>{
    try{
     if(signal.aborted){reject(new Error('已取消导出'));return}
     canvas.width=width;canvas.height=height
     const context=canvas.getContext('2d');if(!context)throw new Error('浏览器不支持图片转换')
     context.drawImage(image,0,0)
     canvas.toBlob(result=>result?resolve(result):reject(new Error('图片转换失败')),'image/png')
    }catch(error){reject(error)}
   }
   image.onerror=()=>reject(new Error('图片转换失败'));image.src=url
  })
  if(signal.aborted)throw new Error('已取消导出')
  return {blob,filename:agentExportName(snapshot.day,'png')}
 }catch(error){if(signal.aborted)throw error;return fallback}
 finally{clearTimeout(timer);if(abort)signal.removeEventListener('abort',abort);image.onload=null;image.onerror=null;image.src='';URL.revokeObjectURL(url);canvas.width=0;canvas.height=0}
}

export function saveAgentExport(file:AgentExportFile):void {
 const url=URL.createObjectURL(file.blob),link=document.createElement('a')
 link.href=url;link.download=file.filename;document.body.appendChild(link)
 try{link.click()}finally{link.remove();setTimeout(()=>URL.revokeObjectURL(url),60000)}
}
