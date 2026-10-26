import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {parse} from '@vue/compiler-sfc'
import {computed,ref} from 'vue'
const read=file=>readFileSync(new URL('../'+file,import.meta.url),'utf8')
test('my team excludes deeper levels and stale account data',()=>{
 const user=ref({id:'member'}),account=ref({user:{id:'member'},members:[{id:'direct',level:1,balance_cents:100,today_stake_cents:200},{id:'grandchild',level:2},{id:'third',level:3}]})
 const source=parse(read('src/components/MyTeam.vue')).descriptor.scriptSetup.content+'\nexport {team}'
 const context={exports:{},require:name=>name==='vue'?{computed}:{user,account,money:String}}
 vm.runInNewContext(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
 assert.deepEqual(Array.from(context.exports.team.value,x=>x.id),['direct'])
 user.value={id:'other'};assert.equal(context.exports.team.value.length,0)
 account.value={user:{id:'other'},members:[]};assert.equal(context.exports.team.value.length,0)
 assert.doesNotMatch(read('src/components/InviteFriends.vue'),/account\?\.members|invite-team/)
 assert.match(read('src/components/AccountPanel.vue'),/emit\('navigate','team'\)/)
})
test('Beijing calendar dates use UTC+8 independently of browser locale',()=>{
 const context={exports:{},Date}
 vm.runInNewContext(ts.transpileModule(read('src/lib/beijingDate.ts'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
 assert.equal(context.exports.beijingToday(Date.parse('2026-10-26T15:59:59Z')),'2026-10-26')
 assert.equal(context.exports.beijingToday(Date.parse('2026-10-26T16:00:00Z')),'2026-10-27')
})
