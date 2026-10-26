import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {parse} from '@vue/compiler-sfc'
import {computed,nextTick,reactive,ref,watch} from 'vue'
const read=file=>readFileSync(new URL('../'+file,import.meta.url),'utf8')
const helpers={exports:{}};vm.runInNewContext(ts.transpileModule(read('src/lib/withdrawalAccounts.ts'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,helpers)
const settle=async()=>{await nextTick();await new Promise(resolve=>setImmediate(resolve))}
const trc='T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb',erc='0x'+'a'.repeat(40)
function addressHarness(loadReply,saveReply=async(network,version,address)=>({network,version:version+1,address}),initial={}){
 const user=ref({id:'member'}),props=reactive({network:'TRC20',address:'',locked:false,...initial}),events=[],stops=[],unmount=[],calls=[]
 const source=parse(read('src/components/WalletWithdrawalAddress.vue')).descriptor.scriptSetup.content+'\nexport {load,save,selectSaved,saved,editing,loaded,loading,saving,error}'
 const context={exports:{},Error,defineProps:()=>props,defineEmits:()=>(event,value)=>{events.push([event,value]);if(event==='update:address')props.address=value;if(event==='update:network')props.network=value},require:name=>{
  if(name==='vue')return {ref,computed,watch(...args){const stop=watch(...args);stops.push(stop);return stop},onUnmounted:fn=>unmount.push(fn)}
  if(name.includes('api/account'))return {user}
  if(name.includes('lib/withdrawalAccounts'))return helpers.exports
  if(name.includes('api/wallet'))return {loadWithdrawalAddresses:()=>loadReply(user.value?.id),saveWithdrawalAddress:(...args)=>{calls.push(args);return saveReply(...args)}}
  throw Error(name)
 }}
 vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
 return {s:context.exports,user,props,events,calls,close(){stops.forEach(stop=>stop());unmount.forEach(fn=>fn())}}
}

test('saved addresses refill by account and network, allow edits and restore on cancel',async()=>{
 const h=addressHarness(async()=>[{version:1,network:'TRC20',address:trc},{version:2,network:'ERC20',address:erc}])
 try{
  await settle();assert.equal(h.props.address,trc);assert.equal(h.s.editing.value,false)
  h.props.network='ERC20';await settle();assert.equal(h.props.address,erc)
  h.s.editing.value=true;h.props.address='0x'+'b'.repeat(40);h.s.selectSaved();assert.equal(h.props.address,erc)
  h.s.editing.value=true;h.props.address='0x'+'b'.repeat(40);await h.s.save()
  assert.equal(h.calls[0][0],'ERC20');assert.equal(h.calls[0][1],2);assert.equal(h.s.editing.value,false)
  h.props.network='TRC20';await settle();assert.equal(h.props.address,trc)
 }finally{h.close()}
})

test('wallet account types save and refill independently while USDT addresses remain available',async()=>{
 const h=addressHarness(async()=>[{version:1,network:'TRC20',address:trc}])
 try{
  await settle()
  for(const network of ['OKPAY','TOPAY','KDPAY','FPAY','808']){
   h.props.network=network;await settle();assert.equal(h.props.address,'')
   h.props.address='001-'+network;await h.s.save()
   assert.equal(h.calls.at(-1)[0],network);assert.equal(h.s.editing.value,false)
  }
  h.props.network='TRC20';await settle();assert.equal(h.props.address,trc)
  h.props.network='OKPAY';await settle();assert.equal(h.props.address,'001-OKPAY')
  h.props.address='contains space';await h.s.save();assert.match(h.s.error.value,/不能含空白/)
  assert.equal(h.calls.length,5)
 }finally{h.close()}
})

test('late responses never fill another account or an unmounted editor',async()=>{
 const responses=[];const h=addressHarness(owner=>new Promise(resolve=>responses.push({owner,resolve})))
 h.user.value={id:'other'};await settle()
 responses[0].resolve([{version:1,network:'TRC20',address:trc}]);await settle();assert.equal(h.props.address,'')
 responses[1].resolve([{version:1,network:'TRC20',address:'T'+'A'.repeat(33)}]);await settle();assert.equal(h.props.address,'T'+'A'.repeat(33))
 const result=h.s.load();h.close();responses[2].resolve([{network:'TRC20',version:2,address:trc}]);await result;assert.notEqual(h.props.address,trc)
})

test('saved preference cannot replace the address in a pending request',async()=>{
 const pendingAddress='T'+'B'.repeat(33)
 const h=addressHarness(async()=>[{version:3,network:'TRC20',address:trc}],undefined,{locked:true,address:pendingAddress})
 try{await settle();assert.equal(h.props.address,pendingAddress);await h.s.save();assert.equal(h.calls.length,0);assert.equal(h.props.address,pendingAddress)}finally{h.close()}
})

test('failed saves preserve the edited address and return a retryable error',async()=>{
 const h=addressHarness(async()=>[{network:'TRC20',version:1,address:trc}],async()=>{throw Error('网络异常')})
 try{await settle();h.s.editing.value=true;h.props.address='T'+'C'.repeat(33);await h.s.save();assert.equal(h.s.saving.value,false);assert.match(h.s.error.value,/网络异常/);assert.equal(h.props.address,'T'+'C'.repeat(33));assert.equal(h.s.saved.value[0].address,trc)}finally{h.close()}
})

test('late save from a previous account does not publish a success or keep the new account locked',async()=>{
 let resolveSave
 const h=addressHarness(async()=>[],()=>new Promise(resolve=>{resolveSave=resolve}))
 try{await settle();h.props.address=trc;const pending=h.s.save();h.user.value={id:'other'};await settle();resolveSave({network:'TRC20',version:1,address:trc});await pending;assert.equal(h.s.saving.value,false);assert.equal(h.props.address,'');assert.ok(!h.events.some(([event])=>event==='notify'))}finally{h.close()}
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

test('wallet integrates saved addresses and only USDT QR display without browser storage',()=>{
 const wallet=read('src/components/WalletPanel.vue'),admin=read('src/components/AdminPaymentSettings.vue'),address=read('src/components/WalletWithdrawalAddress.vue')
 assert.match(wallet,/<WalletWithdrawalForm v-if="kind==='withdraw'/);assert.match(read('src/components/WalletWithdrawalForm.vue'),/<WalletWithdrawalAddress/);assert.match(wallet,/addressSaving/)
 assert.match(wallet,/activePayment\?\.qr_code_data_url/);assert.doesNotMatch(wallet,/人工存款|人工提款/)
 assert.doesNotMatch(address,/localStorage|sessionStorage/)
 assert.doesNotMatch(admin,/label="人工存款二维码"/);assert.match(admin,/label="USDT充值二维码"/)
 for(const file of ['WalletPanel.vue','WalletWithdrawalAddress.vue','PaymentQRCodeEditor.vue','AdminPaymentSettings.vue'])assert.equal(parse(read('src/components/'+file)).errors.length,0)
})
