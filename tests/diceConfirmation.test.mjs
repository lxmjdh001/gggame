import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {parse} from '@vue/compiler-sfc'
import {ref,computed,reactive,watch,nextTick} from 'vue'
const source=parse(readFileSync(new URL('../src/components/MarketBoard.vue',import.meta.url),'utf8')).descriptor.scriptSetup.content
const settle=async()=>{await nextTick();await new Promise(resolve=>setImmediate(resolve))}
const market=(id,label)=>({id,group:'双面',label,category:'two_sides',kind:'size',positions:[],pick:id,rule:'fixture',rule_version:1,default_odds_milli:1980})
function harness(reply=async()=>({orders:[{}],total_cost_cents:1000}),overrides={}){
 const user=ref({id:'member',username:'test',balance_cents:100000}),props=reactive({sessionId:'k3-1',targetIssue:'202610272000',blocked:false,compact:true,dice:true,playSetting:{unit_cents:200,max_stake_cents:100000},...overrides}),events=[],posts=[],stops=[],unmount=[]
 const book={session_id:'k3-1',markets:[market('big','大'),market('small','小')],quotes:[{market_id:'big',odds_milli:1980,enabled:true},{market_id:'small',odds_milli:1980,enabled:true}]}
 class APIError extends Error{constructor(message,status){super(message);this.status=status}}
 let keys=0
 const context={exports:{},Error,Map,Set,AbortController,setTimeout,clearTimeout,clearInterval,defineProps:()=>props,defineEmits:()=>(...args)=>events.push(args),require:name=>{
  if(name==='vue')return {ref,computed,watch(...args){const stop=watch(...args);stops.push(stop);return stop},onMounted:()=>{},onUnmounted:fn=>unmount.push(fn)}
  if(name.includes('api/account'))return {user,APIError,key:()=>`fixture-key-${++keys}`,money:v=>(v/100).toFixed(2),refreshAccount:async()=>{},api:async(path,method,payload)=>{posts.push({path,method,payload:JSON.parse(JSON.stringify(payload))});return reply(payload)}}
  if(name.includes('api/markets'))return {oddsText:v=>(v/1000).toFixed(3)}
  if(name.includes('lib/stake'))return {parseStake:value=>/^\d+(\.\d{1,2})?$/.test(value.trim())?Math.round(Number(value)*100):0}
  if(name.includes('lib/marksixOdds'))return {selectionOdds:(m,q)=>q?.odds_milli??0}
  if(name.includes('lib/marksix'))return {quoteID:m=>m.quote_id??m.id,usesZodiac:()=>false}
  if(name.includes('lib/marketOdds'))return {copyTierOdds:v=>v?{...v}:undefined,sameTierOdds:(a,b)=>JSON.stringify(a)===JSON.stringify(b),validTierOdds:()=>true}
  throw Error(name)
 },fetch:async()=>({ok:true,json:async()=>({data:book})})}
 vm.runInNewContext(ts.transpileModule(source+'\nexport {requestSubmit,confirmDiceSubmit,select,remove,resetSelection,confirmQuotes,chosen,book,amount,total,busy,locked,retryPayload,diceConfirmOpen,submitDisabled}',{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
 return {s:context.exports,user,props,events,posts,fixture:book,close(){stops.forEach(stop=>stop());unmount.forEach(fn=>fn())}}
}
test('dice opens confirmation without posting; cancel keeps picks and only confirm submits',async()=>{
 const h=harness(async payload=>({orders:payload.selections.map(()=>({})),total_cost_cents:payload.selections.length*payload.amount_cents}))
 try{
  await settle();h.s.select(h.fixture.markets[0]);h.s.select(h.fixture.markets[1]);h.s.requestSubmit();assert.equal(h.s.diceConfirmOpen.value,true);assert.equal(h.posts.length,0)
  h.s.diceConfirmOpen.value=false;assert.equal(h.s.chosen.value.length,2);h.s.confirmDiceSubmit();await settle();assert.equal(h.posts.length,0)
  h.s.requestSubmit();h.s.amount.value='20';await settle();assert.equal(h.s.total.value,4000);h.s.confirmDiceSubmit();await settle()
  assert.equal(h.posts.length,1);assert.equal(h.posts[0].payload.amount_cents,2000);assert.equal(h.posts[0].payload.selections.length,2);assert.equal(h.s.diceConfirmOpen.value,false);assert.equal(h.s.chosen.value.length,0)
 }finally{h.close()}
})
test('in-flight confirmation cannot submit a second batch',async()=>{
 let resolve
 const h=harness(()=>new Promise(done=>resolve=done))
 try{await settle();h.s.select(h.fixture.markets[0]);h.s.requestSubmit();h.s.confirmDiceSubmit();h.s.confirmDiceSubmit();assert.equal(h.posts.length,1);assert.equal(h.s.busy.value,true);resolve({orders:[{}],total_cost_cents:1000});await settle();assert.equal(h.s.diceConfirmOpen.value,false)}finally{h.close()}
})
test('period rollover, final selection removal and owner change close stale confirmation before any post',async()=>{
 const h=harness()
 try{
  await settle();h.s.select(h.fixture.markets[0]);h.s.requestSubmit();h.props.targetIssue='202610272001';await settle();assert.equal(h.s.diceConfirmOpen.value,false);assert.equal(h.s.chosen.value.length,0)
  h.s.select(h.fixture.markets[0]);h.s.requestSubmit();h.s.remove('big');assert.equal(h.s.diceConfirmOpen.value,false)
  h.s.select(h.fixture.markets[0]);h.s.requestSubmit();h.user.value={id:'other',balance_cents:100000};assert.equal(h.s.diceConfirmOpen.value,false);assert.equal(h.s.chosen.value.length,0);assert.equal(h.posts.length,0)
 }finally{h.close()}
})
test('blocked periods and changed odds cannot bypass validation inside confirmation',async()=>{
 const h=harness()
 try{
  await settle();h.s.select(h.fixture.markets[0]);h.s.requestSubmit();h.props.blocked=true;h.s.confirmDiceSubmit();await settle();assert.equal(h.posts.length,0)
  h.props.blocked=false;h.s.book.value.quotes[0].odds_milli=1990;h.s.confirmDiceSubmit();await settle();assert.equal(h.posts.length,0);assert.equal(h.s.submitDisabled.value,true)
  h.s.confirmQuotes();h.s.confirmDiceSubmit();await settle();assert.equal(h.posts.length,1);assert.equal(h.posts[0].payload.selections[0].odds_milli,1990)
 }finally{h.close()}
})
test('uncertain responses retry the original key, amount and issue after the issue rolls',async()=>{
 let attempt=0
 const h=harness(async()=>{if(attempt++===0)throw Error('network');return {orders:[{}],total_cost_cents:1000}})
 try{
  await settle();h.s.select(h.fixture.markets[0]);h.s.requestSubmit();h.s.confirmDiceSubmit();await settle();assert.equal(h.s.locked.value,true)
  h.props.targetIssue='202610272002';h.props.blocked=true;h.s.book.value.quotes[0].odds_milli=2000;await settle();h.s.diceConfirmOpen.value=false;h.s.requestSubmit();h.s.confirmDiceSubmit();await settle()
  assert.equal(h.posts.length,2);assert.deepEqual(h.posts[1].payload,h.posts[0].payload);assert.equal(h.posts[1].payload.issue,'202610272000');assert.equal(h.s.locked.value,false)
 }finally{h.close()}
})
test('other non-confirmed market games keep their original submit path',async()=>{
 const h=harness(undefined,{dice:false})
 try{await settle();h.s.select(h.fixture.markets[0]);h.s.requestSubmit();await settle();assert.equal(h.s.diceConfirmOpen.value,false);assert.equal(h.posts.length,1)}finally{h.close()}
})
