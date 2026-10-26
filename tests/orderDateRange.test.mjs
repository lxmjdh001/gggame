import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {parse,compileScript} from '@vue/compiler-sfc'
import * as vue from 'vue'

const source=readFileSync(new URL('../src/components/MyOrders.vue',import.meta.url),'utf8')
const descriptor=parse(source).descriptor
const settle=async()=>{await vue.nextTick();await new Promise(resolve=>setImmediate(resolve));await vue.nextTick()}
const data=(extra={})=>({items:[],total:0,page:1,page_size:10,total_stake_cents:0,total_payout_cents:0,...extra})
const deferred=()=>{let resolve,reject;const promise=new Promise((yes,no)=>{resolve=yes;reject=no});return {promise,resolve,reject}}
const params=path=>Object.fromEntries(new URL(path,'http://test').searchParams)
function evaluate(code,require){
 const context={exports:{},require,Error,URLSearchParams,defineEmits:()=>()=>{}}
 vm.runInNewContext(ts.transpileModule(code,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,context)
 return context.exports
}
function harness(reply=async()=>data()){
 const user=vue.ref({id:'member'}),account=vue.ref(null),calls=[],stops=[],unmount=[]
 const state=evaluate(descriptor.scriptSetup.content+'\nexport {load,filter,applyDates,clearDates,validDate,startDate,endDate,appliedStart,appliedEnd,status,orders,page,total,stake,payout,loading,error,dateError,hasFilters,filterOpen}',name=>{
  if(name==='vue')return {...vue,watch(...args){const stop=vue.watch(...args);stops.push(stop);return stop},onUnmounted:fn=>unmount.push(fn)}
  if(name.includes('/api/account'))return {user,account,api:path=>{calls.push(path);return reply(path)},money:String}
  if(name.includes('issueDisplay'))return {formatIssue:String}
  if(name==='../data')return {displayGameName:String}
  if(name==='lucide-vue-next')return {}
  throw Error(name)
 })
 return {s:state,user,account,calls,close(){stops.forEach(stop=>stop());unmount.forEach(fn=>fn())}}
}

test('dates apply on submission at page one; polling and pagination retain applied dates rather than drafts',async()=>{
 const h=harness(async path=>data({page:Number(params(path).page),total:27,total_stake_cents:2700,total_payout_cents:5400}))
 try{
  await settle();await h.s.load(3)
  h.s.startDate.value='2024-02-29';h.s.endDate.value='2024-03-01'
  assert.equal(h.calls.length,2,'editing dates does not submit')
  h.s.applyDates();assert.equal(h.s.page.value,1);await settle()
  assert.deepEqual(params(h.calls.at(-1)),{page:'1',start_date:'2024-02-29',end_date:'2024-03-01'})
  assert.equal(h.s.total.value,27);assert.equal(h.s.stake.value,2700);assert.equal(h.s.payout.value,5400)
  await h.s.load(2);h.s.startDate.value='2025-01-01';h.s.endDate.value='2025-01-02'
  h.account.value={};await settle()
  assert.deepEqual(params(h.calls.at(-1)),{page:'2',start_date:'2024-02-29',end_date:'2024-03-01'})
  h.s.filter('win');await settle()
  assert.deepEqual(params(h.calls.at(-1)),{page:'1',status:'win',start_date:'2024-02-29',end_date:'2024-03-01'})
  h.s.clearDates();await settle()
  assert.deepEqual(params(h.calls.at(-1)),{page:'1',status:'win'})
  assert.equal(h.s.startDate.value,'');assert.equal(h.s.endDate.value,'')
 }finally{h.close()}
})

test('strict calendar validation includes leap centuries, optional endpoints and reversed ranges',async()=>{
 const h=harness()
 try{
  await settle()
  for(const valid of ['', '2024-02-29','2000-02-29','0001-01-01','9999-12-31'])assert.equal(h.s.validDate(valid),true,valid)
  for(const invalid of ['2023-02-29','1900-02-29','2024-04-31','2024-00-01','2024-13-01','2024-01-00','2024-2-29','0000-01-01','10000-01-01',' 2024-02-29','2024-02-29T00:00:00Z']){
   for(const field of ['startDate','endDate']){
    h.s.startDate.value='';h.s.endDate.value='';h.s[field].value=invalid
    const count=h.calls.length;h.s.applyDates();await settle()
    assert.equal(h.calls.length,count,invalid);assert.match(h.s.dateError.value,/有效日期/)
   }
  }
  h.s.startDate.value='2024-03-01';h.s.endDate.value='2024-02-29'
  const count=h.calls.length;h.s.applyDates();await settle()
  assert.equal(h.calls.length,count);assert.match(h.s.dateError.value,/开始日期不能晚于结束日期/)
  assert.equal(h.s.appliedStart.value,'');assert.equal(h.s.appliedEnd.value,'')
  for(const [start,end] of [['2024-02-29','2024-02-29'],['2024-02-29',''],['','2024-02-29']]){
   h.s.startDate.value=start;h.s.endDate.value=end;h.s.applyDates();await settle()
   const query=params(h.calls.at(-1));assert.equal(query.start_date??'',start);assert.equal(query.end_date??'',end)
   assert.equal(h.s.dateError.value,'');assert.equal(h.s.hasFilters.value,true)
  }
 }finally{h.close()}
})

test('newer filters win over stale successes and failures without clearing the current loading state',async()=>{
 let pending=false;const requests=[]
 const h=harness(()=>{if(!pending)return Promise.resolve(data());const d=deferred();requests.push(d);return d.promise})
 try{
  await settle();pending=true
  const old=h.s.load(2)
  h.s.startDate.value='2024-02-29';h.s.applyDates()
  requests[0].resolve(data({items:[{id:'stale'}],total:999,page:2}));await old
  assert.equal(h.s.orders.value.length,0);assert.equal(h.s.total.value,0);assert.equal(h.s.loading.value,true)
  requests[1].resolve(data({total:9,total_stake_cents:900}));await settle()
  assert.equal(h.s.total.value,9);assert.equal(h.s.loading.value,false)
  const failed=h.s.load(1);h.s.filter('pending')
  requests[2].reject(Error('stale failure'));await failed
  assert.equal(h.s.error.value,'');assert.equal(h.s.loading.value,true)
  requests[3].resolve(data());await settle();assert.equal(h.s.loading.value,false)
 }finally{h.close()}
})

test('switching accounts synchronously clears old records, summaries, drafts and filters and rejects old responses',async()=>{
 let pending=false;const requests=[]
 const h=harness(()=>{if(!pending)return Promise.resolve(data({items:[{id:'private'}],total:11,total_stake_cents:123,total_payout_cents:456}));const d=deferred();requests.push(d);return d.promise})
 try{
  await settle();h.s.filter('win');await settle()
  h.s.startDate.value='2024-02-29';h.s.endDate.value='2024-03-01';h.s.applyDates();await settle()
  pending=true;const old=h.s.load(2)
  h.user.value={id:'other'}
  assert.equal(h.s.orders.value.length,0);assert.equal(h.s.total.value,0);assert.equal(h.s.stake.value,0);assert.equal(h.s.payout.value,0)
  for(const key of ['status','startDate','endDate','appliedStart','appliedEnd','dateError','error'])assert.equal(h.s[key].value,'',key)
  assert.equal(h.s.page.value,1);assert.deepEqual(params(h.calls.at(-1)),{page:'1'})
  // Return to the original identity before its old request resolves (A -> B -> A).
  h.user.value={id:'member'}
  requests[0].resolve(data({items:[{id:'old-private'}],total:999}));await old
  requests[1].reject(Error('other account failure'));await settle()
  assert.equal(h.s.total.value,0);assert.equal(h.s.error.value,'');assert.equal(h.s.loading.value,true)
  requests[2].resolve(data());await settle()
  h.user.value=null;const count=h.calls.length;h.s.applyDates();await h.s.load();assert.equal(h.calls.length,count)
 }finally{h.close()}
})

test('failed date submission clears previous data, can retry the applied query, and empty results remain filtered',async()=>{
 let fail=false
 const h=harness(async()=>{if(fail)throw Error('网络异常');return data()})
 try{
  await settle();fail=true;h.s.endDate.value='2024-02-29';h.s.applyDates();await settle()
  assert.match(h.s.error.value,/网络异常/);assert.equal(h.s.orders.value.length,0);assert.equal(h.s.total.value,0);assert.equal(h.s.loading.value,false)
  fail=false;await h.s.load()
  assert.deepEqual(params(h.calls.at(-1)),{page:'1',end_date:'2024-02-29'})
  assert.equal(h.s.error.value,'');assert.equal(h.s.hasFilters.value,true);assert.equal(h.s.orders.value.length,0)
 }finally{h.close()}
})

test('unmount rejects an outstanding response',async()=>{
 const d=deferred(),h=harness(()=>d.promise)
 h.close();d.resolve(data({items:[{id:'private'}],total:99}));await settle()
 assert.equal(h.s.orders.value.length,0);assert.equal(h.s.total.value,0)
})

test('compiled member form wires date inputs, submit prevention, validation feedback and clear action',async()=>{
 // Render the real compiled SFC with Vue's host-independent renderer. Native
 // v-model's DOM directive is replaced; invoke its actual model-update callback.
 const calls=[],user=vue.ref({id:'member'}),account=vue.ref(null)
 const component=evaluate(compileScript(descriptor,{id:'order-range-test',inlineTemplate:true}).content,name=>{
  if(name==='vue')return {...vue,vModelText:{}}
  if(name.includes('/api/account'))return {user,account,api:async path=>{calls.push(path);return data()},money:String}
  if(name.includes('issueDisplay'))return {formatIssue:String}
  if(name==='../data')return {displayGameName:String}
  if(name==='lucide-vue-next')return Object.fromEntries(['ChevronLeft','ChevronRight','Filter','RefreshCw'].map(name=>[name,()=>null]))
  throw Error(name)
 }).default
 const node=(type,text='')=>({type,text,props:{},children:[],parent:null})
 const renderer=vue.createRenderer({
  createElement:node,createText:text=>node('#text',text),createComment:text=>node('#comment',text),
  setText:(el,text)=>{el.text=text},setElementText:(el,text)=>{el.text=text;el.children=[]},
  parentNode:el=>el.parent,nextSibling:el=>el.parent?.children[el.parent.children.indexOf(el)+1]??null,
  patchProp:(el,key,prev,value)=>{el.props[key]=value},
  insert(el,parent,anchor=null){if(el.parent)el.parent.children.splice(el.parent.children.indexOf(el),1);el.parent=parent;const index=anchor?parent.children.indexOf(anchor):-1;parent.children.splice(index<0?parent.children.length:index,0,el)},
  remove(el){if(el.parent)el.parent.children.splice(el.parent.children.indexOf(el),1)},
 })
 const root=node('root'),app=renderer.createApp(component)
 const all=(el=root)=>[el,...el.children.flatMap(child=>all(child))]
 const text=(el=root)=>el.text+el.children.map(child=>text(child)).join('')
 const find=predicate=>all().find(predicate)
 try{
  app.mount(root);await settle()
  find(el=>el.props['aria-label']==='筛选投注记录').props.onClick();await settle()
  const form=find(el=>el.type==='form'),inputs=all().filter(el=>el.type==='input')
  assert.equal(inputs.length,2);assert.match(form.props['aria-label'],/北京时间/)
  inputs[0].props['onUpdate:modelValue']('2024-03-01');inputs[1].props['onUpdate:modelValue']('2024-02-29')
  let prevented=0;form.props.onSubmit({preventDefault(){prevented++}});await settle()
  assert.equal(prevented,1);assert.equal(calls.length,1);assert.match(text(),/开始日期不能晚于结束日期/)
  inputs[0].props['onUpdate:modelValue']('2024-02-29')
  form.props.onSubmit({preventDefault(){prevented++}});await settle()
  assert.deepEqual(params(calls.at(-1)),{page:'1',start_date:'2024-02-29',end_date:'2024-02-29'})
  assert.match(text(),/暂无符合条件的投注记录/)
  find(el=>el.type==='button'&&text(el)==='清除日期').props.onClick();await settle()
  assert.deepEqual(params(calls.at(-1)),{page:'1'});assert.match(text(),/暂无投注记录/);assert.doesNotMatch(text(),/开始日期不能晚于结束日期/)
 }finally{app.unmount()}
})
