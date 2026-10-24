import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {parse} from '@vue/compiler-sfc'
import {ref,watch,nextTick} from 'vue'

const read=path=>readFileSync(new URL('../'+path,import.meta.url),'utf8')
function transpile(source,require=()=>{throw new Error('Unexpected import')},extra={}){
 const context={exports:{},require,Error,performance,...extra}
 vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
 return context.exports
}
const helper=transpile(read('src/lib/secondaryPassword.ts'))
const locked=()=>({is_set:true,enabled:true,verification_required:true,server_time:new Date().toISOString()})
const granted=(ttl=3600000)=>({...locked(),verification_required:false,verified_until:new Date(Date.now()+ttl).toISOString()})
const settle=async()=>{await nextTick();await new Promise(resolve=>setImmediate(resolve));await nextTick()}

test('secondary password accepts 6–12 characters with no case composition rule',()=>{
 for(const value of ['123456','abcdef','ABCDEF','abcdefghijkl','密码六位测试','😀'.repeat(12)])assert.equal(helper.validSecondaryPassword(value),true,value)
 for(const value of ['', '12345','a'.repeat(13),'😀'.repeat(13)])assert.equal(helper.validSecondaryPassword(value),false,value)
})

test('grant TTL uses server clock, fails closed, and never extends beyond an hour',()=>{
 assert.equal(helper.gameAccessTTL(locked()),0)
 assert.equal(helper.gameAccessTTL({...locked(),enabled:false}),Infinity)
 const state=granted(1000)
 assert.equal(helper.gameAccessTTL(state),1000)
 assert.equal(helper.gameAccessTTL({...state,server_time:state.verified_until}),0)
 assert.equal(helper.gameAccessTTL(granted(7200000)),3600000)
 assert.equal(helper.gameAccessTTL({...state,is_set:false}),0)
 assert.throws(()=>helper.gameAccessTTL({}),/状态读取失败/)
 assert.throws(()=>helper.gameAccessTTL({...state,verified_until:undefined}),/有效期读取失败/)
})

function gateHarness(reply){
 const account={user:ref({id:'member',secondary_password_set:true,secondary_password_enabled:true}),authReady:ref(true),gameAccessChallenge:ref(0),api:async(...args)=>reply(...args)}
 const timers=new Map(),intervals=new Map(),mount=[],unmount=[],listeners=new Map(),stops=[]
 const source=parse(read('src/components/GameAccessGate.vue')).descriptor.scriptSetup.content+'\nexport {check,verify,unlocked,hasEntered,checking,busy,error,password,state}'
 let seq=0
 const state=transpile(source,name=>{
  if(name==='vue')return {ref,watch(...args){const stop=watch(...args);stops.push(stop);return stop},onMounted:f=>mount.push(f),onUnmounted:f=>unmount.push(f)}
  if(name.includes('/api/account'))return account
  if(name.includes('/lib/secondaryPassword'))return helper
  if(name==='lucide-vue-next')return {}
  throw new Error(name)
 },{defineProps:()=>({}),defineEmits:()=>()=>{},setTimeout:(fn,delay)=>{const id=++seq;timers.set(id,{fn,delay});return id},clearTimeout:id=>timers.delete(id),setInterval:(fn,delay)=>{const id=++seq;intervals.set(id,{fn,delay});return id},clearInterval:id=>intervals.delete(id),document:{visibilityState:'visible',addEventListener:(name,fn)=>listeners.set(name,fn),removeEventListener:name=>listeners.delete(name)}})
 mount.forEach(f=>f())
 return {state,account,timers,intervals,listeners,close(){unmount.forEach(f=>f());stops.forEach(f=>f())}}
}

test('first entry is locked; verification unlocks; hourly expiry preserves mounted order state',async()=>{
 let status=locked()
 const h=gateHarness(async path=>path.endsWith('/verify')?(status=granted()):status)
 try{
  await settle();assert.equal(h.state.unlocked.value,false);assert.equal(h.state.hasEntered.value,false)
  h.state.password.value='123456';await h.state.verify()
  assert.equal(h.state.unlocked.value,true);assert.equal(h.state.hasEntered.value,true);assert.equal(h.state.password.value,'')
  assert.equal(h.timers.size,1)
  status=locked();const timer=[...h.timers.values()][0];assert.ok(timer.delay<=3600000);timer.fn();await settle()
  assert.equal(h.state.unlocked.value,false);assert.equal(h.state.hasEntered.value,true,'do not destroy pending request/idempotency state')
 }finally{h.close()}
 assert.equal(h.timers.size,0);assert.equal(h.intervals.size,0);assert.equal(h.listeners.size,0)
})

test('poll failures lock the room and late responses cannot reopen an unmounted gate',async()=>{
 let failed=false,resolve
 const h=gateHarness(()=>failed?Promise.reject(new Error('offline')):Promise.resolve(granted()))
 await settle();assert.equal(h.state.unlocked.value,true)
 failed=true;await h.state.check();assert.equal(h.state.unlocked.value,false);assert.match(h.state.error.value,/offline/)
 failed=false
 h.account.api=()=>new Promise(r=>{resolve=r})
 const pending=h.state.check();h.close();resolve(granted());await pending;await settle()
 assert.equal(h.state.unlocked.value,false)
})

test('login identity and server-side required challenges force a fresh check',async()=>{
 let status=granted()
 const h=gateHarness(async()=>status)
 try{
  await settle();assert.equal(h.state.unlocked.value,true)
  status=locked();h.account.user.value={id:'other',secondary_password_set:true,secondary_password_enabled:true};await settle()
  assert.equal(h.state.unlocked.value,false)
  status=granted();await h.state.check();assert.equal(h.state.unlocked.value,true)
  status=locked();h.account.gameAccessChallenge.value++;await settle();assert.equal(h.state.unlocked.value,false)
 }finally{h.close()}
})

test('returning from a backgrounded browser locks immediately until the server rechecks',async()=>{
 const h=gateHarness(async()=>granted())
 try{
  await settle();assert.equal(h.state.unlocked.value,true)
  h.listeners.get('visibilitychange')()
  assert.equal(h.state.unlocked.value,false)
  await settle();assert.equal(h.state.unlocked.value,true)
 }finally{h.close()}
})

test('secondary-required API errors preserve login and trigger room revalidation',async()=>{
 const account=transpile(read('src/api/account.ts'),name=>{
  if(name==='vue')return {ref,computed:fn=>({value:fn()})}
  if(name.includes('requestTimeout'))return {withRequestTimeout:fn=>fn(undefined)}
  throw new Error(name)
 },{fetch:async()=>({ok:false,status:403,json:async()=>({error:{code:'SECONDARY_PASSWORD_REQUIRED',message:'需要二级密码'}})})})
 account.user.value={id:'member'}
 await assert.rejects(account.api('/market-orders','POST',{}),e=>e.status===403&&e.code==='SECONDARY_PASSWORD_REQUIRED')
 assert.equal(account.user.value.id,'member');assert.equal(account.gameAccessChallenge.value,1)
})

test('profile exposes secondary settings and every real game room is wrapped in the gate',()=>{
 const profile=read('src/components/AccountPanel.vue'),app=read('src/App.vue'),gate=read('src/components/GameAccessGate.vue'),settings=read('src/components/SecondaryPasswordSettings.vue')
 assert.match(profile,/二级密码/);assert.match(profile,/<SecondaryPasswordSettings/)
 assert.match(app,/<GameAccessGate[^>]*>[\s\S]*?<GameRoom[\s\S]*?<\/GameAccessGate>/)
 assert.match(gate,/v-if="hasEntered" v-show="unlocked"/)
 for(const file of [gate,settings])assert.doesNotMatch(file,/(localStorage|sessionStorage)/)
 assert.match(settings,/role="switch"/);assert.match(settings,/当前二级密码/);assert.match(settings,/首次设置请确认登录密码/)
 for(const file of ['GameAccessGate.vue','SecondaryPasswordSettings.vue'])assert.equal(parse(read('src/components/'+file)).errors.length,0)
})

function settingsHarness(initial){
 const requests=[],stops=[],unmount=[]
 const member={user:ref({id:'member'}),api:async(path,method='GET',body)=>{
  requests.push({path,method,body})
  if(method==='GET')return initial
  if(path.endsWith('/toggle'))return {...initial,enabled:body.enabled,verification_required:body.enabled}
  return {...initial,is_set:true,enabled:initial.is_set?initial.enabled:true,verification_required:true}
 }}
 const source=parse(read('src/components/SecondaryPasswordSettings.vue')).descriptor.scriptSetup.content+'\nexport {save,state,loading,busy,error,loginPassword,currentPassword,newPassword,confirmation}'
 const state=transpile(source,name=>{
  if(name==='vue')return {ref,watch(...args){const stop=watch(...args);stops.push(stop);return stop},onUnmounted:f=>unmount.push(f)}
  if(name.includes('/api/account'))return member
  if(name.includes('/lib/secondaryPassword'))return helper
  if(name==='lucide-vue-next')return {}
  throw new Error(name)
 },{defineEmits:()=>()=>{}})
 return {state,requests,member,close(){unmount.forEach(f=>f());stops.forEach(f=>f())}}
}

test('first setup validates confirmation, login proof and length before submission',async()=>{
 const h=settingsHarness({...locked(),is_set:false,enabled:false,verification_required:false})
 try{
  await settle()
  const s=h.state
  s.newPassword.value='12345';s.confirmation.value='12345';await s.save();assert.match(s.error.value,/6–12/)
  s.newPassword.value='123456';s.confirmation.value='654321';await s.save();assert.match(s.error.value,/不一致/)
  s.confirmation.value='123456';await s.save();assert.match(s.error.value,/登录密码/)
  assert.equal(h.requests.length,1,'invalid forms must not transmit a mutation')
  s.loginPassword.value='synthetic-login';await s.save()
  assert.equal(s.state.value.enabled,true)
  assert.equal(h.requests.at(-1).body.new_password,'123456')
  for(const field of ['loginPassword','currentPassword','newPassword','confirmation'])assert.equal(s[field].value,'')
 }finally{h.close()}
})

test('toggle requires the current secondary password and never requires a replacement password',async()=>{
 const h=settingsHarness(locked())
 try{
  await settle();await h.state.save(true);assert.match(h.state.error.value,/当前二级密码/)
  assert.equal(h.requests.length,1)
  h.state.currentPassword.value='synthetic';await h.state.save(true)
  assert.equal(h.requests.at(-1).body.enabled,false)
  assert.equal(h.state.state.value.enabled,false)
  assert.equal(h.member.user.value.secondary_password_enabled,false)
  assert.equal(h.state.currentPassword.value,'')
 }finally{h.close()}
})

test('OpenAPI exposes authenticated 6–12 character secondary endpoints',()=>{
 const spec=JSON.parse(read('backend/docs/openapi.json'))
 for(const suffix of ['/status','','/verify','/toggle']){
  const path=spec.paths['/api/v1/auth/secondary-password'+suffix]
  assert.ok(path)
  const op=path[suffix==='/status'?'get':'post']
  assert.deepEqual(op.security,[{SessionCookie:[]}])
 }
 const fields=spec.paths['/api/v1/auth/secondary-password'].post.requestBody.content['application/json'].schema.properties
 assert.equal(fields.new_password.minLength,6);assert.equal(fields.new_password.maxLength,12);assert.equal(fields.new_password.pattern,undefined)
})
