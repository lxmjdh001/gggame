import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {parse} from '@vue/compiler-sfc'
import {ref} from 'vue'
const read=p=>readFileSync(new URL('../'+p,import.meta.url),'utf8')

test('member and admin labels consistently distinguish the required amount from betting turnover',()=>{
 const wallet=read('src/components/WalletPanel.vue'),editor=read('src/components/AdminWagerEditor.vue'),settings=read('src/components/AdminWithdrawalSettings.vue')
 assert.match(wallet,/<span>打码量<\/span><strong>\{\{money\(account.wagering.required_cents\)/)
 assert.match(wallet,/<span>打码流水<\/span><strong>\{\{money\(account.wagering.turnover_cents\)/)
 assert.match(editor,/<p>打码量 <strong>\{\{money\(progress.required_cents\)/)
 assert.match(editor,/当前打码流水 \{\{money\(progress.turnover_cents\)/)
 assert.match(editor,/<label>调整后打码流水（元）/)
 for(const source of [wallet,editor,settings]){
  assert.equal(parse(source).errors.length,0)
  assert.doesNotMatch(source,/打码额度|调整后打码量|调整打码量/)
 }
 assert.match(editor,/不能在此手动修改/)
 for(const source of [wallet,settings]){
  assert.match(source,/打码流水≥打码量/)
  assert.match(source,/相等也/)
  assert.match(source,/提交后/)
  assert.match(source,/下一轮/)
 }
})

test('admin edits send turnover only, never recharge/reward or required amount',async()=>{
 const calls=[],notices=[]
 const progress={version:1,revision:'1:50000:2000:10000',required_cents:52000,deposit_cents:50000,reward_cents:2000,turnover_cents:10000,green_channel:false}
 const source=parse(read('src/components/AdminWagerEditor.vue')).descriptor.scriptSetup.content+'\nexport {load,save,amount,note}'
 const context={exports:{},Error,document:{},defineProps:()=>({member:{id:'member',username:'test-member'}}),defineEmits:()=>(...args)=>notices.push(args),require:name=>{
  if(name==='vue')return {ref,onMounted:()=>{},onUnmounted:()=>{},nextTick:async()=>{}}
  if(name==='lucide-vue-next')return {}
  if(name.includes('api/account'))return {key:()=> 'idempotency-test-key',cents:value=>Math.round(Number(value)*100),money:()=>'',api:async(path,method='GET',body)=>{calls.push({path,method,body});return progress}}
  throw Error(name)
 }}
 vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
 const s=context.exports
 await s.load();assert.equal(s.amount.value,'100.00')
 s.amount.value='530.00';s.note.value='核实后修改流水';await s.save()
 const sent=calls.find(call=>call.method==='POST')
 assert.equal(sent.path,'/admin/users/member/wagering');assert.equal(sent.body.turnover_cents,53000)
 for(const key of ['required_cents','deposit_cents','reward_cents'])assert.equal(key in sent.body,false)
 assert.ok(notices.some(([event,message])=>event==='notify'&&message.includes('打码流水')))
 assert.equal(progress.required_cents,52000)
})
