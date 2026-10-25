import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import postcss from 'postcss'
import {parse} from '@vue/compiler-sfc'

const read=path=>readFileSync(new URL('../'+path,import.meta.url),'utf8')
const context={exports:{},Number,Math}
vm.runInNewContext(ts.transpileModule(read('src/lib/authViewport.ts'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}}).outputText,context)
const {observeAuthViewport}=context.exports
function target(values={}){
 const listeners=new Map()
 return {...values,listeners,addEventListener(name,fn){if(!listeners.has(name))listeners.set(name,new Set());listeners.get(name).add(fn)},removeEventListener(name,fn){listeners.get(name)?.delete(fn)},emit(name){listeners.get(name)?.forEach(fn=>fn())}}
}

test('legacy Android without visualViewport uses actual innerHeight and reacts to rotation',()=>{
 const host=target({innerHeight:640,visualViewport:null}),heights=[]
 const stop=observeAuthViewport(height=>heights.push(height),host)
 assert.deepEqual(heights,[640]);host.innerHeight=360;host.emit('orientationchange');assert.deepEqual(heights,[640,360])
 host.innerHeight=0;host.emit('resize');host.innerHeight=NaN;host.emit('resize');assert.deepEqual(heights,[640,360])
 stop();assert.equal([...host.listeners.values()].every(set=>set.size===0),true)
})
test('keyboard changes the dialog bound, pinch zoom is not overridden, and cleanup ignores stale events',()=>{
 const viewport=target({height:639.7,scale:1}),host=target({innerHeight:720,visualViewport:viewport}),heights=[]
 const stop=observeAuthViewport(height=>heights.push(height),host)
 viewport.height=310;viewport.emit('resize');viewport.emit('resize');assert.deepEqual(heights,[639,310])
 viewport.scale=2;viewport.height=160;viewport.emit('resize');assert.deepEqual(heights,[639,310])
 viewport.scale=1;viewport.height=640;viewport.emit('resize');assert.deepEqual(heights,[639,310,640])
 const stale=[...viewport.listeners.get('resize')][0];stop();viewport.height=200;stale();assert.deepEqual(heights,[639,310,640])
 assert.equal([...viewport.listeners.values()].every(set=>set.size===0),true)
})
test('invalid visual viewport measurements fall back to innerHeight',()=>{
 const viewport=target({height:0,scale:1}),host=target({innerHeight:600,visualViewport:viewport}),heights=[]
 const stop=observeAuthViewport(height=>heights.push(height),host);assert.deepEqual(heights,[600]);stop()
})
test('registration remains bounded and scrollable when dvh and inset declarations are unsupported',()=>{
 const {descriptor,errors}=parse(read('src/components/AuthDialog.vue'));assert.equal(errors.length,0)
 const css=postcss.parse(descriptor.styles[0].content)
 const declarations=(selector,mobile=false)=>{
  const result={}
  css.walkRules(rule=>{
   if(rule.selector!==selector||Boolean(rule.parent.type==='atrule')!==mobile)return
   rule.walkDecls(decl=>{if(!decl.value.includes('dvh')&&decl.prop!=='inset')result[decl.prop]=decl.value})
  });return result
 }
 const overlay=declarations('.auth-overlay'),panel=declarations('.auth-panel'),mobile=declarations('.auth-panel',true),body=declarations('.auth-body')
 assert.equal(overlay.top,'0');assert.equal(overlay.bottom,'0');assert.match(overlay.height,/100vh/)
 assert.match(panel['max-height'],/100vh/);assert.equal(mobile.height,'100%');assert.equal(mobile['max-height'],'100%')
 assert.equal(body['min-height'],'0');assert.equal(body.flex,'1 1 auto');assert.equal(body['overflow-y'],'auto');assert.match(body['touch-action'],/pan-y/)
 assert.equal(body['-webkit-overflow-scrolling'],'touch')
 assert.match(descriptor.scriptSetup.content,/stopViewport\?\.\(\)/)
 assert.doesNotMatch(read('src/components/AuthDialog.vue'),/@touch(?:move|start).*prevent/)
})
