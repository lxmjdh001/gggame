import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import {readFileSync} from 'node:fs'
import ts from 'typescript'
import {parse} from '@vue/compiler-sfc'
import {ref,watch,nextTick} from 'vue'
const read=p=>readFileSync(new URL('../'+p,import.meta.url),'utf8')
const settle=async()=>{await nextTick();await new Promise(r=>setImmediate(r))}
function harness(file,reply,props={}) {
 const stop=[],unmount=[],mount=[],calls=[]
 const source=parse(read('src/components/'+file)).descriptor.scriptSetup.content+'\nexport {load,busy,error,'+(file==='AdminAgents.vue'?'data,query,selected,focus,reset':'children,page,expanded,toggle')+'}'
 const context={exports:{},Error,defineProps:()=>props,defineEmits:()=>()=>{},require:n=>{
  if(n==='vue')return {ref,watch(...args){const f=watch(...args);stop.push(f);return f},onMounted:f=>mount.push(f),onUnmounted:f=>unmount.push(f)}
  if(n.includes('api/account'))return {user:ref({id:'admin',role:'admin'}),money:()=>'',datetime:()=>''}
  if(n.includes('api/agents'))return {loadAgents:q=>{calls.push(q);return reply(q)}}
  if(n==='lucide-vue-next'||n.endsWith('.vue'))return {}
  throw Error(n)
 }}
 vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
 mount.forEach(f=>f())
 return {s:context.exports,calls,close(){stop.forEach(f=>f());unmount.forEach(f=>f())}}
}
test('stale searches and unmounted replies never replace the selected tree',async()=>{
 const replies=[];const h=harness('AdminAgents.vue',q=>new Promise(r=>replies.push({q,r})))
 h.s.focus('new-root');replies[1].r({items:[{id:'new'}],total:1});await settle()
 replies[0].r({items:[{id:'old'}],total:1});await settle();assert.equal(h.s.data.value.items[0].id,'new')
 h.s.focus('last');h.close();replies[2].r({items:[{id:'late'}]});await settle();assert.equal(h.s.data.value,null)
})
test('branches lazy-load, page without duplicates, and retain children on collapse',async()=>{
 const h=harness('AdminAgentNode.vue',async q=>({page:q.page,total:3,items:q.page===1?[{id:'one'},{id:'two'}]:[{id:'two'},{id:'three'}]}),{node:{id:'root',direct_count:3}})
 try{
  assert.equal(h.calls.length,0);h.s.toggle();await settle();assert.equal(h.s.children.value.length,2)
  h.s.toggle();h.s.toggle();await settle();assert.equal(h.calls.length,1)
  await h.s.load();assert.deepEqual(Array.from(h.s.children.value,n=>n.id),['one','two','three'])
  assert.equal(h.calls[1].page,2);assert.equal(h.calls[1].parent_id,'root')
 }finally{h.close()}
})
test('failed branches keep an explicit retry and do not advance their page',async()=>{
 const h=harness('AdminAgentNode.vue',async()=>{throw Error('network unavailable')},{node:{id:'root',direct_count:1}})
 try{h.s.toggle();await settle();assert.match(h.s.error.value,/network/);assert.equal(h.s.page.value,0)}finally{h.close()}
})
test('admin tree is reachable through its own backend tab and scrollable chart',()=>{
 const panel=read('src/components/AdminPanel.vue'),tree=read('src/components/AdminAgents.vue')
 assert.match(panel,/id:'agents',label:'代理关系'/);assert.match(panel,/<AdminAgents v-if="tab==='agents'"/)
 assert.match(tree,/role="region"/);assert.match(tree,/tabindex="0"/);assert.match(tree,/overflow:auto/)
 for(const p of ['AdminAgents.vue','AdminAgentNode.vue'])assert.equal(parse(read('src/components/'+p)).errors.length,0)
})
