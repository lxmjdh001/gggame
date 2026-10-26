import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import {parse} from '@vue/compiler-sfc'
import {reactive,ref,watch,nextTick} from 'vue'
const read=path=>readFileSync(new URL('../'+path,import.meta.url),'utf8')
function module(source,globals={}){
 const context={exports:{},Error,Blob,URL,AbortController,setTimeout,clearTimeout,...globals}
 vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
 return context.exports
}
const lib=module(read('src/lib/agentExport.ts'))
const node=(id,parent='',extra={})=>({id,username:id,inviter_id:parent,inviter_username:parent,status:'active',missing_parent:false,deposit_cents:12345,direct_count:0,team_count:0,team_deposit_cents:0,...extra})
const snapshot=items=>({items,day:'2026-10-26',today_stake_cents:12345,exported_at:'2026-10-26T12:00:00+08:00',scope:''})
test('complete image includes all levels and sibling connectors with escaped account text',()=>{
 const data=snapshot([node('root'),node('child','root'),node('grandchild','child'),node('<script>&"\'','grandchild'),node('other','root')])
 const image=lib.agentRelationSVG(data)
 assert.equal((image.svg.match(/<g transform=/g)||[]).length,5)
 assert.equal((image.svg.match(/<path d="M\d/g)||[]).length,9) // Five card accents and four connections.
 assert.match(image.svg,/&lt;script&gt;&amp;&quot;&apos;/);assert.doesNotMatch(image.svg,/<script|foreignObject|href=/)
 assert.match(image.svg,/123\.45 元/);assert.match(image.svg,/共 5 个账号/)
 for(const [,x,y] of image.svg.matchAll(/translate\((\d+) ([\d.]+)\)/g)){assert.ok(+x+310<=image.width);assert.ok(+y+136<=image.height)}
})
test('empty, orphan and very deep trees have complete deterministic layouts, cycles fail explicitly',()=>{
 assert.match(lib.agentRelationSVG(snapshot([])).svg,/暂无代理邀请关系/)
 const deep=Array.from({length:3000},(_,i)=>node('n'+i,i?'n'+(i-1):'missing'))
 const data=snapshot(deep),one=lib.agentRelationSVG(data),two=lib.agentRelationSVG(data)
 assert.equal(one.svg,two.svg);assert.equal((one.svg.match(/<g transform=/g)||[]).length,3000)
 assert.throws(()=>lib.agentRelationSVG(snapshot([node('a','b'),node('b','a')])),/循环/)
 assert.throws(()=>lib.agentRelationSVG(snapshot([node('a'),node('a')])),/重复/)
})
test('oversized image exports full SVG without allocating a canvas; abort prevents generation',async()=>{
 const data=snapshot(Array.from({length:100},(_,i)=>node('n'+i)))
 const file=await lib.agentRelationImage(data,new AbortController().signal)
 assert.match(file.filename,/\.svg$/);assert.match(file.note,/完整 SVG/)
 assert.equal((await file.blob.text()).match(/<g transform=/g).length,100)
 const controller=new AbortController();controller.abort()
 await assert.rejects(lib.agentRelationImage(data,controller.signal),/取消/)
})
test('PNG conversion and failing conversion release image URLs and canvases',async()=>{
 for(const failure of [false,true]){
  const revoked=[],canvas={width:0,height:0,getContext:()=>({drawImage(){}}),toBlob(fn){fn(new Blob(['png'],{type:'image/png'}))}}
  const functions=module(read('src/lib/agentExport.ts'),{URL:{createObjectURL:()=> 'blob:preview',revokeObjectURL:url=>revoked.push(url)},Image:class{set src(value){if(value)queueMicrotask(()=>failure?this.onerror?.():this.onload?.())}},document:{createElement:()=>canvas}})
  const file=await functions.agentRelationImage(snapshot([node('a')]),new AbortController().signal)
  assert.match(file.filename,failure?/\.svg$/:/\.png$/);assert.deepEqual(revoked,['blob:preview']);assert.equal(canvas.width,0);assert.equal(canvas.height,0)
 }
})
test('cancelling while the canvas encoder is pending releases resources immediately',async()=>{
 const revoked=[],controller=new AbortController(),canvas={width:0,height:0,getContext:()=>({drawImage(){}}),toBlob(){}}
 const functions=module(read('src/lib/agentExport.ts'),{URL:{createObjectURL:()=> 'blob:pending',revokeObjectURL:url=>revoked.push(url)},Image:class{set src(value){if(value)queueMicrotask(()=>this.onload?.())}},document:{createElement:()=>canvas}})
 const pending=functions.agentRelationImage(snapshot([node('a')]),controller.signal)
 await new Promise(resolve=>setImmediate(resolve));controller.abort()
 await assert.rejects(pending,/取消/);assert.deepEqual(revoked,['blob:pending']);assert.equal(canvas.width,0)
})
const settle=async()=>{await nextTick();await new Promise(resolve=>setImmediate(resolve))}
function harness(reply){
 const user=ref({id:'admin',role:'admin'}),props=reactive({username:'a',disabled:false}),stops=[],unmount=[],calls=[],saved=[],images=[]
 const source=parse(read('src/components/AdminAgentExport.vue')).descriptor.scriptSetup.content+'\nexport {download,format,busy,error,message}'
 const s=module(source,{defineProps:()=>props,require:name=>{
  if(name==='vue')return {ref,watch(...args){const stop=watch(...args);stops.push(stop);return stop},onUnmounted:fn=>unmount.push(fn)}
  if(name.includes('api/account'))return {user}
  if(name.includes('api/agents'))return {exportAgents:(...args)=>{calls.push(args);return reply(...args)}}
  if(name.includes('lib/agentExport'))return {agentRelationImage:async data=>{images.push(data);return {blob:new Blob(['png']),filename:'test.png'}},saveAgentExport:file=>saved.push(file)}
  if(name==='lucide-vue-next')return {}
  throw Error(name)
 }})
 return {s,user,props,calls,saved,images,close(){stops.forEach(stop=>stop());unmount.forEach(fn=>fn())}}
}
test('format choice exports the applied root without page parameters or repeated clicks',async()=>{
 let finish;const h=harness(()=>new Promise(resolve=>{finish=resolve}))
 try{
  const pending=h.s.download();await h.s.download();assert.equal(h.calls.length,1)
  assert.equal(h.calls[0][0],'a');assert.equal(h.calls[0][1],'xlsx');assert.equal(h.s.busy.value,true)
  finish({blob:new Blob(['xlsx']),filename:'test.xlsx'});await pending;assert.equal(h.saved.length,1)
  h.s.format.value='image';h.props.username='';await settle()
  const image=h.s.download();finish(snapshot([node('a')]));await image
  assert.equal(h.calls[1][0],'');assert.equal(h.calls[1][1],'data');assert.equal(h.images.length,1);assert.equal(h.saved[1].filename,'test.png')
 }finally{h.close()}
})
test('changing scope, refreshing, switching accounts or unmounting cancels stale downloads',async()=>{
 for(const change of [h=>{h.props.username='b'},h=>{h.props.disabled=true},h=>{h.user.value={id:'other',role:'admin'}},h=>h.close()]){
  let finish;const h=harness(()=>new Promise(resolve=>{finish=resolve}))
  const pending=h.s.download(),signal=h.calls[0][2];change(h);await settle()
  assert.equal(signal.aborted,true);finish({blob:new Blob(['private']),filename:'old.xlsx'});await pending
  assert.equal(h.saved.length,0);assert.equal(h.s.message.value,'');h.close()
 }
})
test('same-account background refresh does not cancel an export',async()=>{
 let finish;const h=harness(()=>new Promise(resolve=>{finish=resolve}))
 try{
  const pending=h.s.download();h.user.value={id:'admin',role:'admin'};await settle()
  assert.equal(h.calls[0][2].aborted,false)
  finish({blob:new Blob(['xlsx']),filename:'same-account.xlsx'});await pending;assert.equal(h.saved.length,1)
 }finally{h.close()}
})
test('failed export preserves the chart and offers retry; disabled and non-admin cannot export',async()=>{
 const h=harness(async()=>{throw Error('网络异常')})
 try{
  await h.s.download();assert.match(h.s.error.value,/网络异常/);assert.equal(h.s.busy.value,false)
  h.props.disabled=true;await settle();await h.s.download();assert.equal(h.calls.length,1)
  h.props.disabled=false;h.user.value={id:'member',role:'user'};await settle();await h.s.download();assert.equal(h.calls.length,1)
 }finally{h.close()}
})
test('export API preserves auth errors, validates files and sends no pagination',async()=>{
 const calls=[],user=ref({id:'admin'}),account=ref({user:{id:'admin'}})
 let response={ok:true,headers:new Headers({'Content-Type':'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet','Content-Disposition':'attachment; filename="agent-relations-20261026-120000.xlsx"'}),blob:async()=>new Blob(['xlsx'])}
 const api=module(read('src/api/agents.ts'),{URLSearchParams,fetch:async(...args)=>{calls.push(args);return response},require:name=>{
  if(name==='./account')return {user,account,APIError:class extends Error{constructor(message,status){super(message);this.status=status}}}
  if(name.includes('requestTimeout'))return {withRequestTimeout:fn=>fn(new AbortController().signal)}
  if(name.includes('agentExport'))return lib
  throw Error(name)
 }})
 const file=await api.exportAgents('0001','xlsx',new AbortController().signal)
 assert.match(calls[0][0],/format=xlsx&username=0001$/);assert.doesNotMatch(calls[0][0],/page|parent_id/);assert.equal(calls[0][1].cache,'no-store');assert.equal(calls[0][1].credentials,'same-origin')
 assert.equal(file.filename,'agent-relations-20261026-120000.xlsx')
 response={ok:true,headers:new Headers({'Content-Type':'text/html'})};await assert.rejects(api.exportAgents('','xlsx',new AbortController().signal),/文件异常/)
 response={ok:false,status:401,json:async()=>({error:{message:'登录已过期'}})}
 await assert.rejects(api.exportAgents('','xlsx',new AbortController().signal),/登录已过期/);assert.equal(user.value,null);assert.equal(account.value,null)
})
