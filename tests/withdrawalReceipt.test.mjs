import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {parse} from '@vue/compiler-sfc'
import {ref,reactive,computed,watch,nextTick} from 'vue'
const read=file=>readFileSync(new URL('../'+file,import.meta.url),'utf8')
const helpers={exports:{}}
vm.runInNewContext(ts.transpileModule(read('src/lib/withdrawalDisplay.ts'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,helpers)
const settle=async()=>{await nextTick();await new Promise(done=>setImmediate(done))}

test('names and destinations are masked without inventing missing data',()=>{
 const {maskedName,maskedDestination,withdrawalRemark}=helpers.exports
 assert.equal(maskedName('张春玉'),'***春玉');assert.equal(maskedName('张三'),'*三');assert.equal(maskedName(),'未设置')
 assert.equal(maskedDestination('00123456789'),'*****6789');assert.equal(maskedDestination('123'),'*****');assert.equal(maskedDestination(''),'请选择收款账号')
 assert.match(withdrawalRemark('pending'),/处理中/);assert.match(withdrawalRemark('rejected','账号核对失败'),/已驳回.*账号核对失败/)
})

test('receipt reloads current data but rejects replies from a different owner or an unmounted view',async()=>{
 const user=ref({id:'member'}),account=ref(null),props=reactive({requestId:'request-1'}),pending=[],stops=[],unmount=[]
 const code=parse(read('src/components/WithdrawalReceipt.vue')).descriptor.scriptSetup.content+'\nexport {load,detail,loading,error}'
 const context={exports:{},Error,defineProps:()=>props,defineEmits:()=>()=>{},require:name=>{
  if(name==='vue')return {ref,computed,watch(...args){const stop=watch(...args);stops.push(stop);return stop},onUnmounted:fn=>unmount.push(fn)}
  if(name.includes('api/account'))return {user,account,money:String,datetime:String}
  if(name.includes('api/wallet'))return {loadWithdrawalDetail:id=>new Promise((resolve,reject)=>pending.push({id,resolve,reject}))}
  if(name.includes('withdrawalDisplay'))return helpers.exports
  if(name==='lucide-vue-next')return {}
  throw Error(name)
 }}
 vm.runInNewContext(ts.transpileModule(code,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
 const s=context.exports
 try{
  pending[0].resolve({request:{id:'request-1'},balance_before_cents:12000,balance_after_cents:10000});await settle()
  assert.equal(s.detail.value.balance_before_cents,12000)
  account.value={};await settle();user.value={id:'other'}
  assert.equal(s.detail.value,null);assert.equal(s.loading.value,true)
  pending[1].resolve({request:{id:'private-old'}});await settle();assert.equal(s.detail.value,null)
  pending[2].reject(Error('不可访问'));await settle();assert.match(s.error.value,/不可访问/)
  props.requestId='request-2';assert.equal(s.error.value,'')
  unmount.forEach(fn=>fn());pending[3].resolve({request:{id:'late-private'}});await settle();assert.equal(s.detail.value,null)
 }finally{stops.forEach(stop=>stop())}
})

test('reference withdrawal view keeps the real amount limits and omits unconfigured competitor rules',()=>{
 const source=read('src/components/WalletWithdrawalForm.vue')
 assert.equal(parse(source).errors.length,0)
 for(const label of ['真实姓名','账户余额','提款方式','通道金额限制','提款金额','数量','汇率','手续费','提款密码'])assert.match(source,new RegExp(label))
 assert.match(source,/1 – 1,000,000/);assert.doesNotMatch(source,/每日.*3次|00:00.*23:35|5分钟内到账|免费提款次数/)
 assert.match(read('src/components/WalletPanel.vue'),/const accepted=await api<WalletRequest>/)
 assert.match(read('src/components/WalletPanel.vue'),/receiptId.value=accepted.id/)
})
