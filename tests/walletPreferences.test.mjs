import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {parse} from '@vue/compiler-sfc'
import {computed,nextTick,reactive,ref,watch} from 'vue'
const read=file=>readFileSync(new URL('../'+file,import.meta.url),'utf8')
const modules={}
for(const name of ['withdrawalAccounts','withdrawalDisplay']){const context={exports:{}};vm.runInNewContext(ts.transpileModule(read('src/lib/'+name+'.ts'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context);modules[name]=context.exports}
const settle=async()=>{await nextTick();await new Promise(resolve=>setImmediate(resolve))}
const trc='T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb'
function accountHarness(loadReply=async()=>[],saveReply=async(network,version,address)=>({network,version:version+1,address}),initial={}){
 const user=ref({id:'member',withdrawal_password_set:true}),props=reactive({active:true,network:'TRC20',locked:false,...initial}),events=[],stops=[],unmount=[],calls=[]
 const source=parse(read('src/components/WalletWithdrawalAccounts.vue')).descriptor.scriptSetup.content+'\nexport {load,save,add,edit,select,back,changeNetwork,saved,screen,address,confirmation,password,loading,saving,error}'
 const context={exports:{},Error,defineProps:()=>props,defineEmits:()=>(event,value)=>{events.push([event,value]);if(event==='ready'){props.network=value.network;props.active=false}},require:name=>{
  if(name==='vue')return {ref,computed,watch(...args){const stop=watch(...args);stops.push(stop);return stop},onUnmounted:fn=>unmount.push(fn)}
  if(name.includes('api/account'))return {user}
  if(name.includes('api/wallet'))return {loadWithdrawalAddresses:()=>loadReply(user.value?.id),saveWithdrawalAddress:(...args)=>{calls.push(args);return saveReply(...args)}}
  if(name.includes('withdrawalAccounts'))return modules.withdrawalAccounts
  if(name.includes('withdrawalDisplay'))return modules.withdrawalDisplay
  if(name==='lucide-vue-next')return {}
  throw Error(name)
 }}
 vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
 return {s:context.exports,user,props,events,calls,close(){stops.forEach(stop=>stop());unmount.forEach(fn=>fn())}}
}

test('first withdrawal without a saved account stays on management until a confirmed, PIN-protected save succeeds',async()=>{
 const h=accountHarness()
 try{
  await settle();assert.equal(h.s.screen.value,'manage');assert.equal(h.props.active,true);assert.ok(!h.events.some(([name])=>name==='ready'))
  h.s.add();assert.equal(h.s.screen.value,'edit');h.s.address.value=trc;h.s.password.value='483927'
  h.s.confirmation.value='different';await h.s.save();assert.equal(h.calls.length,0);assert.match(h.s.error.value,/不一致/)
  h.s.confirmation.value=trc;h.s.password.value='';await h.s.save();assert.equal(h.calls.length,0)
  h.s.password.value='483927';await h.s.save();await settle()
  assert.deepEqual(h.calls[0],['TRC20',0,trc,trc,'483927']);assert.equal(h.props.active,false);assert.equal(h.s.password.value,'');assert.equal(h.s.confirmation.value,'')
 }finally{h.close()}
})

test('existing accounts skip setup across visits and choose a saved wallet instead of an empty default',async()=>{
 const h=accountHarness(async()=>[{network:'TRC20',version:0,address:''},{network:'808',version:2,address:'00123456789'}])
 try{
  await settle();assert.equal(h.props.active,false);assert.equal(h.props.network,'808')
  assert.equal(h.events.find(([name])=>name==='ready')[1].address,'00123456789')
  h.props.active=true;await settle();assert.equal(h.s.screen.value,'manage');assert.equal(h.props.active,true)
  h.s.edit(h.s.saved.value[1]);h.s.confirmation.value='old-confirm';h.s.password.value='secret';h.s.changeNetwork('OKPAY')
  assert.equal(h.s.password.value,'');assert.equal(h.s.confirmation.value,'')
 }finally{h.close()}
})

test('failed saves never unlock the withdrawal page or overwrite saved accounts and always clear the payment password',async()=>{
 const h=accountHarness(async()=>[],async()=>{throw Error('提款密码不正确')})
 try{
  await settle();h.s.add();h.s.address.value=trc;h.s.confirmation.value=trc;h.s.password.value='000000';await h.s.save()
  assert.equal(h.props.active,true);assert.equal(h.s.screen.value,'edit');assert.match(h.s.error.value,/不正确/);assert.equal(h.s.password.value,'');assert.equal(h.s.saved.value.length,0);assert.equal(h.s.saving.value,false)
 }finally{h.close()}
})

test('switching owners and unmounting reject stale account replies',async()=>{
 const responses=[],h=accountHarness(owner=>new Promise(resolve=>responses.push({owner,resolve})))
 h.user.value={id:'other',withdrawal_password_set:true};await settle()
 responses[0].resolve([{network:'TRC20',version:1,address:trc}]);await settle();assert.ok(!h.events.some(([name])=>name==='ready'))
 responses[1].resolve([]);await settle();assert.equal(h.s.screen.value,'manage')
 const pending=h.s.load(true);h.close();responses[2].resolve([{network:'TRC20',version:1,address:trc}]);await pending
 assert.ok(!h.events.some(([name])=>name==='ready'))
})

test('late saves cannot publish an old owner account and pending financial requests keep their original destination',async()=>{
 let resolveSave
 const h=accountHarness(async()=>[],()=>new Promise(resolve=>{resolveSave=resolve}))
 try{
  await settle();h.s.add();h.s.address.value=trc;h.s.confirmation.value=trc;h.s.password.value='483927';const pending=h.s.save()
  h.user.value={id:'other',withdrawal_password_set:true};await settle();resolveSave({network:'TRC20',version:1,address:trc});await pending
  assert.equal(h.s.password.value,'');assert.ok(!h.events.some(([name])=>name==='ready'||name==='notify'))
 }finally{h.close()}
 const locked=accountHarness(async()=>[{network:'TRC20',version:1,address:trc}],undefined,{locked:true,active:false})
 try{await settle();assert.ok(!locked.events.some(([name])=>name==='ready'));locked.s.add();await locked.s.save();assert.equal(locked.calls.length,0)}finally{locked.close()}
})

test('account loading errors do not bypass first-use setup',async()=>{
 const h=accountHarness(async()=>{throw Error('网络异常')})
 try{await settle();assert.equal(h.s.screen.value,'error');assert.equal(h.props.active,true);assert.match(h.s.error.value,/网络异常/);assert.ok(!h.events.some(([name])=>name==='ready'))}finally{h.close()}
})

test('QR editor only reads bounded raster files and ignores replies after unmount',()=>{
 const readers=[],events=[],unmount=[]
 class Reader {readAsDataURL(){readers.push(this)}abort(){this.onabort?.()}}
 const props={modelValue:'',label:'USDT充值二维码',disabled:false}
 const source=parse(read('src/components/PaymentQRCodeEditor.vue')).descriptor.scriptSetup.content+'\nexport {select,remove,busy,error}'
 const context={exports:{},FileReader:Reader,defineProps:()=>props,defineEmits:()=>(...args)=>events.push(args),require:name=>{if(name==='vue')return {ref,onUnmounted:fn=>unmount.push(fn)};throw Error(name)}}
 vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
 const s=context.exports,select=file=>s.select({target:{files:[file],value:'test'}})
 select({type:'image/svg+xml',size:12});assert.equal(readers.length,0)
 select({type:'image/png',size:256*1024+1});assert.equal(readers.length,0)
 select({type:'image/png',size:100});readers[0].result='data:image/png;base64,fixture';readers[0].onload();assert.equal(events.find(([event])=>event==='update:modelValue')[1],'data:image/png;base64,fixture')
 select({type:'image/jpeg',size:100});const before=events.filter(([event])=>event==='update:modelValue').length;unmount.forEach(fn=>fn());readers[1].result='late';readers[1].onload();assert.equal(events.filter(([event])=>event==='update:modelValue').length,before)
})

test('wallet uses the account gate and keeps PIN-protected binding separate from the withdrawal form',()=>{
 const wallet=read('src/components/WalletPanel.vue'),form=read('src/components/WalletWithdrawalForm.vue'),accounts=read('src/components/WalletWithdrawalAccounts.vue')
 assert.match(wallet,/<WalletWithdrawalAccounts/);assert.match(wallet,/accountsOpen/);assert.match(wallet,/@accounts="accountsOpen=true"/)
 assert.doesNotMatch(form,/WalletWithdrawalAddress|saveWithdrawalAddress/);assert.doesNotMatch(accounts,/localStorage|sessionStorage/)
 assert.match(accounts,/confirmation/);assert.match(accounts,/password/);assert.match(accounts,/保存后进入提款页面/)
 for(const file of ['WalletPanel.vue','WalletWithdrawalAccounts.vue','WalletWithdrawalForm.vue'])assert.equal(parse(read('src/components/'+file)).errors.length,0)
})
