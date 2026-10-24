import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {parse} from '@vue/compiler-sfc'

const read=path=>readFileSync(new URL('../'+path,import.meta.url),'utf8')
const {outputText}=ts.transpileModule(read('src/lib/passwordPolicy.ts'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2018}})
const context={exports:{},TextEncoder}
vm.runInNewContext(outputText,context)
const {validNewPasswordLength}=context.exports

test('new password accepts exactly 8–16 UTF-8 bytes',()=>{
 for(const length of [0,7,8,9,16,17,128])assert.equal(validNewPasswordLength('a'.repeat(length)),length>=8&&length<=16)
 assert.equal(validNewPasswordLength('Aa中文1234'),true)
 assert.equal(validNewPasswordLength('Aa中文中文中文'),false,'do not let a short JS string exceed the backend byte limit')
})

function input(file,model){
 const {descriptor,errors}=parse(read('src/components/'+file))
 assert.equal(errors.length,0)
 const pending=[descriptor.template.ast]
 while(pending.length){
  const node=pending.pop()
  if(node?.tag==='input'&&node.props?.some(p=>p.type===7&&p.name==='model'&&p.exp?.content===model))return node.props
  pending.push(...(node?.children??[]))
 }
 throw new Error('Missing input: '+file+'/'+model)
}
const attr=(props,name)=>props.find(p=>p.type===6&&p.name===name)?.value?.content
const binding=(props,name)=>props.find(p=>p.type===7&&p.name==='bind'&&p.arg?.content===name)?.exp?.content.replace(/\s/g,'')
function bounds(file,model,min,max){const props=input(file,model);assert.equal(attr(props,'minlength'),String(min));assert.equal(attr(props,'maxlength'),String(max))}

test('registration uses 8–16, login preserves the legacy length allowance',()=>{
 const props=input('AuthDialog.vue','password')
 assert.equal(binding(props,'minlength'),'register?8:undefined')
 assert.equal(binding(props,'maxlength'),'register?16:128')
 assert.match(binding(props,'pattern'),/\{8,16\}/)
 bounds('AuthDialog.vue','confirm',8,16)
})
test('personal and administrative new-password fields use 8–16',()=>{
 bounds('AccountPanel.vue','newPassword',8,16)
 bounds('AccountPanel.vue','confirmPassword',8,16)
 assert.equal(attr(input('AccountPanel.vue','oldPassword'),'maxlength'),'128')
 bounds('AdminMemberManagement.vue','password',8,16)
 bounds('AdminMemberManagement.vue','confirmation',8,16)
})
test('withdrawal password remains six digits',()=>{
 bounds('AuthDialog.vue','withdrawalPassword',6,6)
 for(const model of ['newPin','confirmPin','withdrawalPassword','withdrawalConfirmation'])bounds('WalletPanel.vue',model,6,6)
})
test('published API schema documents the new registration/reset bounds',()=>{
 const api=JSON.parse(read('backend/docs/openapi.json'))
 const registration=api.components.schemas.SecureRegistrationInput.properties.password
 assert.equal(registration.minLength,8);assert.equal(registration.maxLength,16)
 const reset=api.paths['/api/v1/admin/users/{id}/password'].post.requestBody.content['application/json'].schema.properties.new_password
 assert.equal(reset.minLength,8);assert.equal(reset.maxLength,16)
})
