import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'

const source=readFileSync(new URL('../src/lib/marksixOdds.ts',import.meta.url),'utf8')
const {outputText}=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}})
const context={exports:{}}
vm.runInNewContext(outputText,context)
const {selectionOdds,supportsCurrentZodiacOdds,conditionalOddsHint}=context.exports
const chart=zodiac=>({numbers:[{number:1,zodiac}]})
const fixed=(pick,kind='lhc_zodiac')=>({rule_version:6,kind,pick:String(pick)})
const combination=(kind,values)=>({rule_version:6,kind,selection_schema:{type:kind.includes('tail')?'tail':'zodiac'},selection_values:values})

test('current-year price follows issue context, not a hard-coded animal',()=>{
 const q={odds_milli:1850,current_zodiac_odds_milli:1700}
 for(const zodiac of [5,6,7])for(let z=0;z<12;z++)assert.equal(selectionOdds(fixed(z),q,chart(zodiac)),z===zodiac?1700:1850)
 assert.equal(selectionOdds(fixed(6),q),1850)
 assert.equal(supportsCurrentZodiacOdds(fixed(6,'lhc_zodiac_count')),false)
})
test('each concrete group in a multiple selection gets its own actual price',()=>{
 const q={odds_milli:3500,current_zodiac_odds_milli:3000}
 const groups=[[0,1],[0,6],[1,6]].map(values=>combination('lhc_link_zodiac',values))
 assert.deepEqual(groups.map(m=>selectionOdds(m,q,chart(6))),[3500,3000,3000])
 assert.equal(selectionOdds(combination('lhc_miss_zodiac',[6,0]),{odds_milli:3000,current_zodiac_odds_milli:2500},chart(6)),2500)
 assert.equal(selectionOdds(combination('lhc_combined_zodiac',[0,6]),{odds_milli:5300,current_zodiac_odds_milli:4800},chart(6)),4800)
 assert.equal(selectionOdds(combination('lhc_link_zodiac',undefined),q,chart(6)),3500)
})
test('contains-zero tail price is independent of ordinary and zodiac rates',()=>{
 const q={odds_milli:3100,zero_tail_odds_milli:4000}
 assert.equal(selectionOdds(combination('lhc_link_tail',[0,1]),q,chart(6)),4000)
 assert.equal(selectionOdds(combination('lhc_link_tail',[1,2]),q,chart(6)),3100)
 assert.equal(selectionOdds(combination('lhc_link_tail',undefined),q,chart(6)),3100)
 assert.match(conditionalOddsHint(combination('lhc_link_tail',[]),q),/含0尾 4倍/)
})
test('legacy/non-conditional quotes retain their price',()=>{
 assert.equal(selectionOdds(fixed(6),{odds_milli:1881},chart(6)),1881)
 assert.equal(selectionOdds({kind:'number'},undefined),0)
 assert.equal(selectionOdds({kind:'lhc_size'},{odds_milli:1980},chart(6)),1980)
 assert.equal(selectionOdds({kind:'lhc_size'},{odds_milli:1960},chart(6)),1960)
})
