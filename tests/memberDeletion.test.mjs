import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {parse,compileTemplate} from '@vue/compiler-sfc'
import * as vue from 'vue'

const read=file=>readFileSync(new URL('../'+file,import.meta.url),'utf8')
const settle=async()=>{await vue.nextTick();await new Promise(resolve=>setImmediate(resolve))}
const deferred=()=>{let resolve,reject;const promise=new Promise((yes,no)=>{resolve=yes;reject=no});return {promise,resolve,reject}}
const state=(extra={})=>({username:'Member',revision:'revision-1',deleted:false,balance_cents:0,frozen_cents:0,betting_frozen_cents:0,available_commission_cents:0,pending_requests:0,pending_orders:0,direct_count:3,can_delete:true,blockers:[],...extra})
function evaluate(source,require,extra={}){
 const context={exports:{},require,Error,...extra}
 vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText,context)
 return context.exports
}
function harness(file,reply,{props={},exports:names,actor={id:'admin',role:'admin'}}){
 const calls=[],events=[],mount=[],unmount=[],stops=[];let counter=0,closed=false
 const account={user:vue.ref(actor),site:vue.ref(null),sessionNames:vue.ref({}),sessionCatalog:vue.ref([]),labels:{active:'正常',disabled:'已停用',deleted:'已删除'},money:value=>String((value??0)/100),datetime:value=>value,key:()=>`synthetic-deletion-key-${++counter}`,loadSessionNames:async()=>{},refreshSite:async()=>{},refreshAccount:async()=>{},api:async(path,method='GET',body)=>{calls.push({path,method,body});return reply(path,method,body)}}
 const document={activeElement:null}
 const require=name=>{
  if(name==='vue')return {...vue,watch(...args){const stop=vue.watch(...args);stops.push(stop);return stop},onMounted:fn=>mount.push(fn),onUnmounted:fn=>unmount.push(fn)}
  if(name.includes('api/account'))return account
  if(name==='lucide-vue-next'||name.endsWith('.vue')||name.includes('../lib/')||name==='../data')return {}
  throw new Error('Unexpected import '+name)
 }
 const descriptor=parse(read('src/components/'+file)).descriptor
 const s=evaluate(descriptor.scriptSetup.content+'\nexport {'+names+'}',require,{document,defineProps:()=>props,defineEmits:()=>(...event)=>events.push(event)})
 const compiled=compileTemplate({source:descriptor.template.content,filename:file,id:'member-deletion-test',compilerOptions:{expressionPlugins:['typescript']}})
 assert.deepEqual(compiled.errors,[])
 // Inspect conditional VNodes; model directives require a DOM, so inputs use real refs.
 const {render}=evaluate(compiled.code,()=>({...vue,resolveComponent:name=>name,withDirectives:node=>node}))
 return {s,calls,events,account,props,document,mount,render(){return render(vue.proxyRefs({...account,...s,...props}),[])},close(){if(closed)return;closed=true;unmount.forEach(fn=>fn());stops.forEach(fn=>fn())}}
}
function deletionHarness(reply=async()=>state(),options={}){
 return harness('AdminMemberDeletion.vue',reply,{...options,props:vue.reactive({member:{id:'member',username:'Member',role:'user'},action:'delete',...options.props}),exports:'preview,confirmation,note,error,loading,saving,requestKey,blockers,validForm,load,save,close,trap,dialog,authorized'})
}
function panelHarness(reply){return harness('AdminPanel.vue',reply,{exports:'tab,rows,overview,total,page,query,loading,error,isAdmin,pageSize,title,tabs,showDeleted,deletionAction,memberAction,inviterUser,wagerUser,action,busy,fundsRevision,load,changeTab,changeUserView,openDeletion,openAction,emit'})}
function walk(node,predicate){
 if(!node||typeof node!=='object')return []
 return [...(predicate(node)?[node]:[]),...(Array.isArray(node.children)?node.children.flatMap(child=>walk(child,predicate)):[])]
}
function text(node){return typeof node==='string'?node:typeof node?.children==='string'?node.children:Array.isArray(node?.children)?node.children.map(text).join(''):''}
function actionButtons(h){const [actions]=walk(h.render(),node=>node.props?.class==='table-actions');return walk(actions,node=>node.type==='button').map(text)}
const validInput=h=>{h.s.confirmation.value='Member';h.s.note.value='用户申请注销'}

test('delete requires an authoritative preview, exact username and a 2–500 codepoint reason',async()=>{
 const ready=deferred(),h=deletionHarness(()=>ready.promise)
 try{
  validInput(h);await h.s.save();assert.equal(h.calls.length,1)
  ready.resolve(state());await settle()
  h.s.note.value='注销申请'
  for(const name of ['', 'member', ' Member', 'Member ']){h.s.confirmation.value=name;await h.s.save();assert.match(h.s.error.value,/完全一致/)}
  h.s.confirmation.value='Member'
  for(const reason of ['', ' ', '字', '字'.repeat(501), '😀'.repeat(501)]){h.s.note.value=reason;await h.s.save();assert.match(h.s.error.value,/2–500/)}
  assert.equal(h.calls.length,1)
  h.s.note.value='  '+ '😀'.repeat(500)+'  ';await h.s.save()
  const call=h.calls.at(-1)
  assert.equal(call.path,'/admin/users/member');assert.equal(call.method,'DELETE')
  assert.deepEqual(Object.keys(call.body).sort(),['confirm_username','key','note','revision'])
  assert.equal(call.body.confirm_username,'Member');assert.equal(call.body.revision,'revision-1');assert.equal(call.body.note,'😀'.repeat(500));assert.match(call.body.key,/synthetic-deletion-key-/)
  assert.deepEqual(h.events.map(([event])=>event),['notify','saved','close'])
  await h.s.save();assert.equal(h.calls.length,2,'successful forms cannot be sent twice')
 }finally{h.close()}
})

test('each financial/processing blocker and the server decision independently prevent deletion',async()=>{
 const cases=[{balance_cents:1},{balance_cents:-1},{frozen_cents:1},{betting_frozen_cents:1},{available_commission_cents:1},{pending_requests:1},{pending_orders:1},{can_delete:false},{blockers:['服务端阻止原因']},{deleted:true}]
 for(const extra of cases){
  const h=deletionHarness(async()=>state(extra))
  try{await settle();validInput(h);assert.ok(h.s.blockers.value.length);await h.s.save();assert.equal(h.calls.length,1);assert.match(h.s.error.value,/阻止原因/);assert.ok(text(h.render()).includes(h.s.blockers.value[0]))}finally{h.close()}
 }
})

test('restore previews the deleted account and posts the same confirmation contract, without enabling it',async()=>{
 const h=deletionHarness(async()=>state({deleted:true,can_delete:false,blockers:['已删除账号不可再删除','存在未领取佣金'],balance_cents:10,available_commission_cents:100}),{props:{action:'restore'}})
 try{
  await settle();assert.equal(h.calls[0].path,'/admin/users/member/deletion');assert.equal(h.s.blockers.value.length,0)
  assert.match(text(h.render()),/停用状态/);assert.doesNotMatch(text(h.render()),/已删除账号不可再删除|存在未领取佣金/);validInput(h);await h.s.save()
  assert.equal(h.calls[1].path,'/admin/users/member/restore');assert.equal(h.calls[1].method,'POST')
  assert.equal(h.calls[1].body.confirm_username,'Member');assert.equal(h.calls[1].body.revision,'revision-1')
  assert.equal(h.calls.some(call=>call.method==='PATCH'),false)
  assert.match(h.events[0][1],/停用状态/)
 }finally{h.close()}
 const live=deletionHarness(async()=>state(),{props:{action:'restore'}})
 try{await settle();validInput(live);await live.s.save();assert.equal(live.calls.length,1);assert.match(live.s.blockers.value[0],/尚未删除/)}finally{live.close()}
})

test('admin targets, self and non-admin sessions cannot preview or submit',async()=>{
 for(const options of [{props:{member:{id:'other-admin',username:'Admin',role:'admin'}}},{props:{member:{id:'admin',username:'Member',role:'user'}}},{actor:{id:'visitor',role:'user'}},{actor:null}]){
  const h=deletionHarness(async()=>state(),options)
  try{await settle();validInput(h);await h.s.save();assert.equal(h.calls.length,0);assert.equal(h.s.authorized.value,false)}finally{h.close()}
 }
})

test('missing preview fields fail closed and a later successful read clears the error',async()=>{
 let incomplete=true
 const h=deletionHarness(async()=>incomplete?{username:'Member',revision:'v1'}:state())
 try{await settle();assert.equal(h.s.preview.value,null);assert.match(h.s.error.value,/数据不完整/);validInput(h);await h.s.save();assert.equal(h.calls.length,1)
  incomplete=false;await h.s.load();assert.equal(h.s.preview.value.username,'Member');assert.equal(h.s.error.value,'')
 }finally{h.close()}
})

test('duplicate reads and submissions are suppressed and close is blocked while saving',async()=>{
 const read=deferred(),write=deferred(),h=deletionHarness((path,method)=>method==='GET'?read.promise:write.promise)
 try{
  await h.s.load();await h.s.load();assert.equal(h.calls.length,1)
  read.resolve(state());await settle();validInput(h)
  const pending=h.s.save();await h.s.save();await h.s.load();h.s.close()
  assert.equal(h.calls.length,2);assert.equal(h.s.saving.value,true);assert.equal(h.events.length,0)
  write.resolve({deleted:true});await pending;assert.equal(h.s.saving.value,false);assert.equal(h.events.filter(([e])=>e==='saved').length,1)
 }finally{h.close()}
})

test('mutation errors invalidate the preview; retry first re-reads the server revision',async()=>{
 let fail=true,revision=0
 const h=deletionHarness(async(path,method)=>{if(method==='GET')return state({revision:'r'+(++revision)});if(fail)throw new Error('网络连接异常');return {deleted:true}})
 try{
  await settle();validInput(h);await h.s.save();const firstKey=h.calls[1].body.key
  assert.match(h.s.error.value,/网络连接异常/);assert.equal(h.s.preview.value,null);assert.equal(h.s.saving.value,false)
  await h.s.save();assert.equal(h.calls.length,2)
  fail=false;await h.s.load();assert.equal(h.s.confirmation.value,'');assert.equal(h.s.note.value,'');validInput(h);await h.s.save()
  assert.equal(h.calls[3].body.revision,'r2');assert.notEqual(h.calls[3].body.key,firstKey)
 }finally{h.close()}
})

test('preview success and errors from old member/action/account resources cannot overwrite the new preview',async()=>{
 for(const change of [h=>{h.props.member={id:'other',username:'Other',role:'user'}},h=>{h.props.action='restore'},h=>{h.account.user.value={id:'new-admin',role:'admin'}}]){
  for(const reject of [false,true]){
   const old=deferred();let reads=0
   const h=deletionHarness(()=>++reads===1?old.promise:state({username:'Current',revision:'new',deleted:true}))
   try{
    h.s.confirmation.value='old confirmation';h.s.note.value='old reason';change(h)
    assert.equal(h.s.confirmation.value,'');assert.equal(h.s.note.value,'')
    await settle();assert.equal(h.s.preview.value.revision,'new')
    if(reject)old.reject(new Error('stale failure'));else old.resolve(state({revision:'old'}))
    await settle();assert.equal(h.s.preview.value.revision,'new');assert.equal(h.s.error.value,'');assert.equal(h.s.loading.value,false)
   }finally{h.close()}
  }
 }
})

test('resource switches during a mutation discard success and failure events and preserve new requests',async()=>{
 for(const change of [h=>{h.props.member={id:'other',username:'Other',role:'user'}},h=>{h.props.action='restore'},h=>{h.account.user.value={id:'new-admin',role:'admin'}}]){
  for(const reject of [false,true]){
   const write=deferred(),fresh=deferred();let reads=0
   const h=deletionHarness((path,method)=>method!=='GET'?write.promise:++reads===1?state():fresh.promise)
   try{
    await settle();validInput(h);const pending=h.s.save();change(h)
    assert.equal(h.s.preview.value,null);assert.equal(h.s.confirmation.value,'');assert.equal(h.s.saving.value,false);assert.equal(h.s.loading.value,true)
    if(reject)write.reject(new Error('old write failed'));else write.resolve({deleted:true})
    await pending;assert.equal(h.events.length,0);assert.equal(h.s.error.value,'');assert.equal(h.s.loading.value,true)
    fresh.resolve(state({revision:'fresh'}));await settle();assert.equal(h.s.preview.value.revision,'fresh')
   }finally{h.close()}
  }
 }
})

test('logout and unmount independently discard in-flight previews and mutations, including rejections',async()=>{
 for(const transition of ['logout','unmount'])for(const mutation of [false,true])for(const reject of [false,true]){
  const pending=deferred();let reads=0
  const h=deletionHarness((path,method)=>mutation&&method==='GET'&&++reads===1?state():pending.promise)
  if(mutation){await settle();validInput(h)}
  const saving=mutation?h.s.save():null
  if(transition==='logout')h.account.user.value=null;else h.close()
  assert.equal(h.s.preview.value,null);assert.equal(h.s.confirmation.value,'')
  if(reject)pending.reject(new Error('late network failure'));else pending.resolve(state())
  await saving;await settle();assert.equal(h.s.preview.value,null);assert.equal(h.events.length,0);assert.equal(h.s.error.value.includes('late'),false);h.close()
 }
})

test('dialog traps Tab in both directions, handles no enabled fields, and returns focus on unmount',async()=>{
 const h=deletionHarness();let focused='',prevented=0
 const first={focus(){focused='first'}},last={focus(){focused='last'}},prior={isConnected:true,focus(){focused='prior'}}
 const dialog={focus(){focused='dialog'},querySelectorAll(){return [first,last]}}
 try{
  h.document.activeElement=prior;await Promise.all(h.mount.map(fn=>fn()))
  h.s.dialog.value=vue.markRaw(dialog)
  for(const [active,shift,expected] of [[dialog,false,'first'],[dialog,true,'last'],[first,true,'last'],[last,false,'first']]){
   h.document.activeElement=active;h.s.trap({key:'Tab',shiftKey:shift,preventDefault(){prevented++}});assert.equal(focused,expected)
  }
  assert.equal(prevented,4)
  dialog.querySelectorAll=()=>[];h.s.trap({key:'Tab',preventDefault(){prevented++}});assert.equal(focused,'dialog')
  h.close();assert.equal(focused,'prior')
 }finally{h.close()}
 const template=parse(read('src/components/AdminMemberDeletion.vue')).descriptor.template.content
 assert.match(template,/@keydown\.esc\.stop\.prevent="close"/)
})

test('list switches request the correct endpoint at page 1 and reject old success/error responses',async()=>{
 for(const rejected of [false,true]){
  const old=deferred(),h=panelHarness(path=>path.includes('/deleted-users?')?{items:[{id:'deleted',deleted:true}],page:1,total:1}:old.promise)
  try{
   h.s.tab.value='users';h.s.page.value=4;h.s.query.value='名字 & q';const loading=h.s.load()
   h.s.changeUserView(true);assert.equal(h.s.page.value,1);assert.equal(h.s.rows.value.length,0)
   await settle();assert.equal(h.s.rows.value[0].id,'deleted')
   assert.equal(h.calls.at(-1).path,'/admin/deleted-users?page=1&q='+encodeURIComponent('名字 & q'))
   if(rejected)old.reject(new Error('stale list failure'));else old.resolve({items:[{id:'live'}],page:4,total:99})
   await loading;assert.equal(h.s.rows.value[0].id,'deleted');assert.equal(h.s.total.value,1);assert.equal(h.s.error.value,'');assert.equal(h.s.loading.value,false)
   h.s.changeUserView(false);assert.equal(h.calls.at(-1).path,'/admin/users?page=1&q='+encodeURIComponent('名字 & q'))
  }finally{h.close()}
 }
})

test('list account changes, navigation and unmount invalidate old responses and close deletion forms',async()=>{
 for(const transition of ['account','logout','tab','unmount']){
  const old=deferred();let count=0
  const h=panelHarness(()=>++count===1?old.promise:{items:[{id:'new'}],page:1,total:1})
  try{
   h.s.tab.value='users';const pending=h.s.load();h.s.openDeletion('delete',{id:'member',username:'Member',role:'user'})
   if(transition==='account')h.account.user.value={id:'admin-2',role:'admin'}
   if(transition==='logout')h.account.user.value=null
   if(transition==='tab')h.s.changeTab('agents')
   if(transition==='unmount')h.close()
   if(transition!=='unmount')assert.equal(h.s.deletionAction.value,null)
   await settle();old.resolve({items:[{id:'old'}],page:9,total:999});await pending
   assert.equal(h.s.rows.value.some(row=>row.id==='old'),false)
   assert.notEqual(h.s.page.value,9)
  }finally{h.close()}
 }
})

test('rendered deleted rows expose only restore; disabled live rows retain ordinary actions and separate deletion',async()=>{
 const h=panelHarness(async()=>({items:[],page:1,total:0}))
 try{
  h.s.tab.value='users';h.s.rows.value=[{id:'member',username:'Member',role:'user',status:'disabled',deleted:true}]
  assert.deepEqual(actionButtons(h),['恢复用户']);assert.match(text(h.render()),/已删除/)
  h.s.openDeletion('delete',h.s.rows.value[0]);assert.equal(h.s.deletionAction.value,null)
  h.s.openDeletion('restore',h.s.rows.value[0]);assert.equal(h.s.deletionAction.value.action,'restore')
  h.s.deletionAction.value=null;h.s.rows.value[0].deleted=false
  assert.deepEqual(actionButtons(h),['调整余额','打码设置','更改上级','修改密码','停用全部下级','恢复','删除用户'])
  assert.match(text(h.render()),/已停用/);assert.doesNotMatch(text(h.render()),/恢复用户/)
  h.s.openDeletion('restore',h.s.rows.value[0]);assert.equal(h.s.deletionAction.value,null)
  for(const row of [{id:'other-admin',role:'admin'},{id:'admin',role:'user'}]){h.s.openDeletion('delete',row);assert.equal(h.s.deletionAction.value,null);h.s.rows.value=[row];assert.equal(actionButtons(h).includes('删除用户'),false)}
  h.s.openDeletion('delete',{id:'member',username:'Member',role:'user'});assert.equal(h.s.deletionAction.value.action,'delete')
  h.account.user.value={id:'admin',role:'admin',balance_cents:10};assert.equal(h.s.deletionAction.value.action,'delete','refreshing the same account must not reset an open operation')
 }finally{h.close()}
})

test('agent cards and exports label deleted nodes while keeping their invitation relationships',()=>{
 const source=read('src/components/AdminAgentNode.vue')
 assert.match(source,/v-if="node.deleted\|\|node.status==='deleted'"[^>]*>已删除/)
 const {agentRelationSVG}=evaluate(read('src/lib/agentExport.ts'),()=>({}))
 const node={id:'parent',username:'Parent',status:'deleted',deposit_cents:0,direct_count:1,team_count:1,team_deposit_cents:0}
 const {svg}=agentRelationSVG({items:[node,{...node,id:'child',username:'Child',status:'disabled',inviter_id:'parent',inviter_username:'Parent'}],day:'2026-10-26',today_stake_cents:0,exported_at:'now',scope:'全部团队'})
 assert.match(svg,/Parent（已删除）/);assert.doesNotMatch(svg,/Child（已删除）/);assert.match(svg,/<path d="M334 /)
})
