import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {parse} from '@vue/compiler-sfc'
const read=path=>readFileSync(new URL('../'+path,import.meta.url),'utf8')
const context={exports:{}}
vm.runInNewContext(ts.transpileModule(read('src/lib/usdtDeposit.ts'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText,context)
const {validUSDTQuantity,usdtDepositCents}=context.exports

test('USDT decimal quantities preserve all six decimal places and round yuan only at the final cent',()=>{
 for(const [quantity,rate,cents] of [
  ['985.5','6.9',679995],['985.500000','6.9',679995],['985.5','7.005',690343],
  ['0.5','6.9',345],['0.000001','1000000',100],['0.999999','1.000001',100],
  ['1.005','1',101],['1.004999','1',100],['999999.999999','1',100000000],
  ['0.144927','6.9',100],['0.144202','6.9',99],['1','1.005',101],['3','7.005',2102],
 ]){assert.equal(validUSDTQuantity(quantity),true,quantity);assert.equal(usdtDepositCents(quantity,rate),cents,`${quantity} × ${rate}`)}
})
test('invalid precision and non-decimal formats cannot be quoted as a positive USDT quantity',()=>{
 for(const quantity of ['','0','0.000000','1.0000001','01','01.5','.5','1.','-1.5','+1.5','1,5','1e3',' 1.5','NaN','Infinity','99999999999999999999']){
  assert.equal(validUSDTQuantity(quantity),false,quantity);assert.equal(usdtDepositCents(quantity,'6.9'),null,quantity)
 }
 for(const rate of ['0','-1','1e3','1000000.000001','7.0000001'])assert.equal(usdtDepositCents('985.5',rate),null,rate)
 assert.equal(usdtDepositCents('9999999999999999999','1000000'),null)
})
test('deposit quantity input offers a mobile decimal keyboard without native integer-step validation',()=>{
 const {descriptor,errors}=parse(read('src/components/WalletPanel.vue'));assert.deepEqual(errors,[])
 const nodes=[descriptor.template.ast];let input
 while(nodes.length){const node=nodes.pop();if(node?.tag==='input'&&node.props?.some(p=>p.type===7&&p.name==='model'&&p.exp?.content==='usdtAmount'))input=node;nodes.push(...(node?.children??[]))}
 assert.ok(input)
 const attr=name=>input.props.find(p=>p.type===6&&p.name===name)?.value?.content
 assert.equal(attr('inputmode'),'decimal');assert.equal(attr('maxlength'),'26');assert.notEqual(attr('type'),'number')
 assert.match(attr('placeholder'),/支持小数/)
})
