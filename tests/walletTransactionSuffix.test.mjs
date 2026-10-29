import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import {parse} from '@vue/compiler-sfc'

test('USDT deposit requests the complete transaction hash in the input',()=>{
 const source=readFileSync(new URL('../src/components/WalletPanel.vue',import.meta.url),'utf8')
 const {descriptor,errors}=parse(source);assert.equal(errors.length,0)
 const nodes=[descriptor.template.ast];let input
 while(nodes.length){const node=nodes.pop();if(node?.tag==='input'&&node.props?.some(p=>p.type===7&&p.name==='model'&&p.exp?.content==='transactionId'))input=node;nodes.push(...(node?.children??[]))}
 assert.ok(input)
 const attr=name=>input.props.find(p=>p.type===6&&p.name===name)?.value?.content
 assert.equal(attr('minlength'),'64');assert.equal(attr('maxlength'),'66');assert.equal(attr('pattern'),'(0x)?[a-fA-F0-9]{64}')
 assert.equal(attr('placeholder'),'请输入完整交易 ID（交易哈希）')
 const pattern=new RegExp('^'+attr('pattern')+'$');assert.equal(pattern.test('abcde'),false);assert.equal(pattern.test('a'.repeat(64)),true);assert.equal(pattern.test('0x'+'a'.repeat(64)),true)
})
