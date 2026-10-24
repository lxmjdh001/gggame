import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import {parse} from '@vue/compiler-sfc'

test('USDT deposit requests only five hex suffix characters in the input',()=>{
 const source=readFileSync(new URL('../src/components/WalletPanel.vue',import.meta.url),'utf8')
 const {descriptor,errors}=parse(source);assert.equal(errors.length,0)
 const nodes=[descriptor.template.ast];let input
 while(nodes.length){const node=nodes.pop();if(node?.tag==='input'&&node.props?.some(p=>p.type===7&&p.name==='model'&&p.exp?.content==='transactionId'))input=node;nodes.push(...(node?.children??[]))}
 assert.ok(input)
 const attr=name=>input.props.find(p=>p.type===6&&p.name===name)?.value?.content
 assert.equal(attr('minlength'),'5');assert.equal(attr('maxlength'),'5');assert.equal(attr('pattern'),'[a-fA-F0-9]{5}')
 assert.equal(attr('placeholder'),'请输入交易 ID 后5位')
 assert.doesNotMatch(descriptor.scriptSetup.content,/网络的完整交易哈希/)
})
