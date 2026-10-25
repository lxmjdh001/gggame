import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {parse} from '@vue/compiler-sfc'
import {ref,watch,nextTick} from 'vue'
const read=file=>readFileSync(new URL('../'+file,import.meta.url),'utf8')
const settle=async()=>{await nextTick();await new Promise(resolve=>setImmediate(resolve))}
function harness(reply){
 const calls=[],events=[],stops=[],unmount=[];let counter=0
 const source=parse(read('src/components/AdminInviterEditor.vue')).descriptor.scriptSetup.content+'\nexport {load,preview,save,state,plan,parentName,detach,note,error,saving}'
 const context={exports:{},Error,defineProps:()=>({member:{id:'member',username:'member'}}),defineEmits:()=>(...args)=>events.push(args),require:name=>{
  if(name==='vue')return {ref,watch(...args){const stop=watch(...args);stops.push(stop);return stop},nextTick,onMounted:()=>{},onUnmounted:fn=>unmount.push(fn)}
  if(name==='lucide-vue-next')return {}
  if(name.includes('api/account'))return {user:ref({id:'admin'}),key:()=> 'key-for-inviter-'+(++counter),api:async(path,method='GET',body)=>{calls.push({path,method,body});return reply(path,method,body)}}
  throw Error(name)
 }}
 vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
 return {s:context.exports,calls,events,close(){stops.forEach(fn=>fn());unmount.forEach(fn=>fn())}}
}
const state={username:'member',inviter_id:'old',inviter_username:'old',new_inviter_id:'new',new_inviter_username:'new',descendants:2,revision:'topology-revision'}
test('changing an inviter requires a fresh preview, reason and server revision',async()=>{
 const h=harness(async()=>state)
 try{await settle();await h.s.save();assert.equal(h.calls.length,1)
  h.s.parentName.value='new';await settle();await h.s.preview();assert.ok(h.s.plan.value)
  await h.s.save();assert.match(h.s.error.value,/操作原因/)
  h.s.note.value='核实迁移团队';await h.s.save();const sent=h.calls.at(-1)
  assert.equal(sent.path,'/admin/users/member/inviter');assert.equal(sent.body.revision,'topology-revision');assert.equal(sent.body.inviter_username,'new');assert.equal(sent.body.detach,false);assert.ok(sent.body.key.length>=16)
  assert.ok(h.events.some(([event])=>event==='saved'))
 }finally{h.close()}
})
test('editing the proposed parent invalidates prior confirmation and stale preview replies',async()=>{
 let resolvePreview;const h=harness(async(path)=>path.endsWith('/preview')?new Promise(resolve=>{resolvePreview=resolve}):state)
 try{await settle();h.s.parentName.value='new';await settle();const pending=h.s.preview();h.s.parentName.value='other';await settle();resolvePreview(state);await pending;assert.equal(h.s.plan.value,null);await h.s.save();assert.ok(!h.events.some(([event])=>event==='saved'))}finally{h.close()}
})
test('compact relationship cards contain only the five requested data fields and daily turnover is outside the card',()=>{
 const card=read('src/components/AdminAgentNode.vue'),page=read('src/components/AdminAgents.vue'),panel=read('src/components/AdminPanel.vue')
 const labels=[...card.matchAll(/<dt>([^<]+)<\/dt>/g)].map(match=>match[1])
 assert.deepEqual(labels,['用户名','上级','个人累计充值','直属 / 全部下级','下级累计充值'])
 assert.doesNotMatch(card,/node\.name|datetime\(|node\.created_at/)
 assert.match(page,/今日流水（含本人）/);assert.match(page,/compact=ref\(true\)/);assert.match(panel,/更改上级/)
 assert.match(panel,/<AdminInviterEditor/)
 for(const file of ['AdminAgentNode.vue','AdminAgents.vue','AdminInviterEditor.vue'])assert.equal(parse(read('src/components/'+file)).errors.length,0)
})
