import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {parse} from '@vue/compiler-sfc'
import {ref,computed,watch,nextTick} from 'vue'
const read=p=>readFileSync(new URL('../'+p,import.meta.url),'utf8')

test('member and admin labels consistently distinguish the required amount from betting turnover',()=>{
 const wallet=read('src/components/WalletWithdrawalForm.vue'),editor=read('src/components/AdminWagerEditor.vue'),settings=read('src/components/AdminWithdrawalSettings.vue')
 assert.match(wallet,/<span>提款所需打码量<\/span><strong>\{\{money\(wager.required_cents\)/)
 assert.match(wallet,/<span>已达打码流水<\/span><strong>\{\{money\(wager.turnover_cents\)/)
 assert.match(editor,/<p>打码量 <strong>\{\{money\(progress.required_cents\)/)
 assert.match(editor,/当前打码流水 \{\{money\(progress.turnover_cents\)/)
 assert.match(editor,/<label>调整后打码流水（元）/)
 for(const source of [wallet,editor,settings]){
  assert.equal(parse(source).errors.length,0)
  assert.doesNotMatch(source,/打码额度/)
 }
 assert.match(editor,/<label>调整后打码量（元）/)
 assert.match(editor,/将打码流水设为0会同时清零打码量/)
 assert.doesNotMatch(editor,/不能在此手动修改/)
 for(const source of [wallet,settings]){
  assert.match(source,/打码流水≥打码量/)
  assert.match(source,/相等也/)
  assert.match(source,/提交后/)
  assert.match(source,/下一轮/)
 }
})

function editorHarness(progress,postReply=async()=>progress){
 const calls=[],notices=[]
 const stops=[],unmount=[]
 const source=parse(read('src/components/AdminWagerEditor.vue')).descriptor.scriptSetup.content+'\nexport {load,save,amount,requiredAmount,requiredInput,clearing,clearBoth,green,note,error,saving}'
 const context={exports:{},Error,document:{},defineProps:()=>({member:{id:'member',username:'test-member'}}),defineEmits:()=>(...args)=>notices.push(args),require:name=>{
  if(name==='vue')return {ref,computed,watch(...args){const stop=watch(...args);stops.push(stop);return stop},onMounted:()=>{},onUnmounted:fn=>unmount.push(fn),nextTick}
  if(name==='lucide-vue-next')return {}
  if(name.includes('api/account'))return {key:()=> 'idempotency-test-key',cents:value=>{if(!/^-?\d+(\.\d{1,2})?$/.test(value))throw Error('金额最多保留两位小数');return Math.round(Number(value)*100)},money:()=>'',api:async(path,method='GET',body)=>{calls.push({path,method,body});return method==='POST'?postReply(body):progress}}
  throw Error(name)
 }}
 vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
 return {s:context.exports,calls,notices,close(){stops.forEach(stop=>stop());unmount.forEach(fn=>fn())}}
}
const fixture=(extra={})=>({version:1,revision:'1:50000:2000:10000:52000',required_cents:52000,deposit_cents:50000,reward_cents:2000,turnover_cents:10000,green_channel:false,...extra})

test('turnover-only edits leave required amount out of the request and never modify original credits',async()=>{
 const progress=fixture(),h=editorHarness(progress)
 try{
  await h.s.load();assert.equal(h.s.amount.value,'100.00');assert.equal(h.s.requiredInput.value,'520.00')
  h.s.amount.value='530.00';h.s.note.value='核实后修改流水';await h.s.save()
  const sent=h.calls.find(call=>call.method==='POST')
  assert.equal(sent.path,'/admin/users/member/wagering');assert.equal(sent.body.turnover_cents,53000)
  for(const key of ['required_cents','deposit_cents','reward_cents'])assert.equal(key in sent.body,false)
  assert.ok(h.notices.some(([event,message])=>event==='notify'&&message.includes('打码设置')))
  assert.equal(progress.required_cents,52000)
 }finally{h.close()}
})

test('required-only editing at zero turnover omits turnover and preserves the green-channel setting',async()=>{
 const h=editorHarness(fixture({turnover_cents:0,green_channel:true}))
 try{
  await h.s.load();h.s.requiredInput.value='600.00';h.s.note.value='核实后单独修改打码量';await h.s.save()
  const {body}=h.calls.find(call=>call.method==='POST')
  assert.equal(body.required_cents,60000);assert.equal('turnover_cents' in body,false);assert.equal(body.green_channel,true)
  assert.equal(h.s.clearing.value,false)
 }finally{h.close()}
})

test('explicit zero shows linked zero and saves both values; undoing restores the independent amount draft',async()=>{
 const h=editorHarness(fixture())
 try{
  await h.s.load();h.s.requiredInput.value='650.00';h.s.amount.value='0'
  assert.equal(h.s.clearing.value,true);assert.equal(h.s.requiredInput.value,'0.00')
  h.s.amount.value='100.00';await nextTick();assert.equal(h.s.clearing.value,false);assert.equal(h.s.requiredInput.value,'650.00')
  h.s.amount.value='0';h.s.note.value='核实后两项清零';await h.s.save()
  const {body}=h.calls.find(call=>call.method==='POST');assert.equal(body.turnover_cents,0);assert.equal(body.required_cents,0)
 }finally{h.close()}
})

test('clear-both is only a draft and also works when turnover was already zero',async()=>{
 const h=editorHarness(fixture({turnover_cents:0}))
 try{
  await h.s.load();h.s.clearBoth();assert.equal(h.s.clearing.value,true);assert.equal(h.calls.filter(c=>c.method==='POST').length,0)
  await h.s.save();assert.match(h.s.error.value,/调整原因/);assert.equal(h.calls.filter(c=>c.method==='POST').length,0)
  h.s.note.value='后台确认清零';await h.s.save()
  const {body}=h.calls.find(c=>c.method==='POST');assert.equal(body.required_cents,0);assert.equal(body.turnover_cents,0)
 }finally{h.close()}
})

test('invalid or negative amount cannot submit and unchanged zero turnover is never implicitly cleared',async()=>{
 const h=editorHarness(fixture({turnover_cents:0}))
 try{
  await h.s.load();h.s.note.value='核实开关';h.s.requiredInput.value='-1';await h.s.save();assert.match(h.s.error.value,/不能为负数/)
  h.s.requiredInput.value='abc';await h.s.save();assert.equal(h.calls.filter(c=>c.method==='POST').length,0)
  h.s.requiredInput.value='520.00';h.s.green.value=true;await h.s.save()
  const {body}=h.calls.find(c=>c.method==='POST');assert.equal('turnover_cents' in body,false);assert.equal('required_cents' in body,false);assert.equal(body.green_channel,true)
 }finally{h.close()}
})

test('failed and duplicate saves retain drafts, and an unmounted editor emits no stale success',async()=>{
 let resolve
 const h=editorHarness(fixture(),()=>new Promise(done=>{resolve=done}))
 await h.s.load();h.s.requiredInput.value='600';h.s.note.value='核实打码量';const pending=h.s.save();await h.s.save()
 assert.equal(h.calls.filter(c=>c.method==='POST').length,1);h.close();resolve({});await pending;assert.equal(h.notices.length,0)
 const failed=editorHarness(fixture(),async()=>{throw Error('网络异常')})
 try{await failed.s.load();failed.s.requiredInput.value='600';failed.s.note.value='网络错误保留草稿';await failed.s.save();assert.match(failed.s.error.value,/网络异常/);assert.equal(failed.s.requiredInput.value,'600');assert.equal(failed.s.saving.value,false);assert.equal(failed.notices.length,0)}finally{failed.close()}
})
