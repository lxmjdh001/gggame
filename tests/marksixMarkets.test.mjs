import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'

const source=readFileSync(new URL('../src/lib/marksix.ts',import.meta.url),'utf8')
const {outputText}=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}})
const context={exports:{}}
vm.runInNewContext(outputText,context)
const {markSixBettingGroup,markSixBettingGroups}=context.exports
const market=(id,kind,ui_group='special')=>({id,kind,ui_group})

test('special page keeps number grid and the requested two-sided/color markets',()=>{
 for(let n=1;n<=49;n++)assert.equal(markSixBettingGroup(market(`p7.n${n}`,'number')),'special')
 for(const kind of ['lhc_size','lhc_parity','lhc_digit_size','lhc_digit_parity','lhc_tail_size','lhc_color']){
  assert.equal(markSixBettingGroup(market('lhc6.special.any',kind)),'special')
 }
})
test('the eight extra special/sum families are absent from betting presentation',()=>{
 for(const kind of ['lhc_size_parity','lhc_zodiac_group','lhc_half_half_wave','lhc_head','lhc_tail','lhc_element']){
  assert.equal(markSixBettingGroup(market('lhc6.special.extra',kind)),null,kind)
 }
 assert.equal(markSixBettingGroup(market('lhc6.total_zodiac.n2','lhc_total_zodiac','sum')),null)
 assert.equal(markSixBettingGroup(market('lhc6.seven_wave.red','lhc_seven_wave','sum')),null)
})
test('only the four total size/parity markets move to regular, never to special',()=>{
 for(const [pick,kind] of [['big','lhc_size'],['small','lhc_size'],['odd','lhc_parity'],['even','lhc_parity']]){
  assert.equal(markSixBettingGroup(market(`lhc6.sum.${pick}`,kind,'sum')),'regular')
 }
 assert.equal(markSixBettingGroups.some(group=>group.id==='sum'),false)
})
test('separate half-wave/zodiac/position offerings remain available and contracts are not mutated',()=>{
 for(const [kind,group] of [['lhc_half_wave','half_wave'],['lhc_half_digit','half_wave'],['lhc_zodiac','special_zodiac'],['lhc_tail_size','position_faces'],['number','position_numbers']]){
  const m=Object.freeze(market('original-id',kind,group))
  assert.equal(markSixBettingGroup(m),group)
  assert.equal(m.id,'original-id')
 }
 assert.equal(markSixBettingGroup(market('lhc6.link.two_special','lhc_link_numbers','linked_numbers')),null)
 assert.equal(markSixBettingGroup(market('p1.n1','number','position_number')),'position_numbers')
})
