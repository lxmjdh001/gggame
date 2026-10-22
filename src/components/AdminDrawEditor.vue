<script setup lang="ts">
import {computed,nextTick,onMounted,onUnmounted,ref} from 'vue'
import {X} from 'lucide-vue-next'
import {api,datetime} from '../api/account'
import type {Round} from '../api/games'
export interface NumberRule {count:number;min:number;max:number;allow_repeat:boolean}
const props=defineProps<{session:{session_id:string;name:string;issue:string;draw_at:string;round:Round;number_rule:NumberRule};clockOffset:number}>()
const emit=defineEmits<{close:[];saved:[];refresh:[]}>()
const inputs=ref(props.session.round.numbers.map(String)),busy=ref(false),error=ref(''),now=ref(Date.now()),dialog=ref<HTMLElement|null>(null)
const cutoff=computed(()=>Date.parse(props.session.draw_at)-6000)
const locked=computed(()=>now.value+props.clockOffset>=cutoff.value)
const rule=computed(()=>props.session.number_rule)
const unchanged=computed(()=>inputs.value.every((value,i)=>value.trim()!==''&&Number(value)===props.session.round.numbers[i]))
let active=true,tick:ReturnType<typeof setInterval>|undefined,priorFocus:HTMLElement|null=null
function close(){if(!busy.value)emit('close')}
function trap(event:KeyboardEvent){
 if(event.key!=='Tab')return
 const nodes=dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled)')
 if(!nodes?.length){event.preventDefault();return}
 const first=nodes[0]!,last=nodes[nodes.length-1]!,outside=!dialog.value?.contains(document.activeElement)
 if(event.shiftKey&&(outside||document.activeElement===first||document.activeElement===dialog.value)){event.preventDefault();last.focus()}
 else if(!event.shiftKey&&(outside||document.activeElement===last||document.activeElement===dialog.value)){event.preventDefault();first.focus()}
}
async function save(){
 now.value=Date.now()
 if(busy.value||locked.value||unchanged.value)return
 error.value=''
 if(inputs.value.length!==rule.value.count||inputs.value.some(value=>!/^\d{1,2}$/.test(value.trim()))){error.value=`请填写完整的${rule.value.count}个整数号码`;return}
 const numbers=inputs.value.map(value=>Number(value.trim()))
 if(numbers.some(n=>n<rule.value.min||n>rule.value.max)){error.value=`号码必须在${rule.value.min}至${rule.value.max}之间`;return}
 if(!rule.value.allow_repeat&&new Set(numbers).size!==numbers.length){error.value='该场次号码不可重复';return}
 busy.value=true
 let applying=false
 try{
  const draft=await api<{candidate:{id:string;round:Round}|null}>(`/admin/game-control/${props.session.session_id}/preview`,'POST',{issue:props.session.issue,numbers,expected_numbers:props.session.round.numbers})
  if(!active)return
  now.value=Date.now()
  if(locked.value)throw new Error('已到操作截止时间，不可修改')
  if(!draft.candidate)throw new Error('未能生成修改预览，请重试')
  applying=true
  await api<Round>(`/admin/game-control/${props.session.session_id}/apply`,'POST',{issue:props.session.issue,candidate_id:draft.candidate.id})
  if(active)emit('saved')
 }catch(e){if(active){error.value=(e as Error).message;if(applying){error.value+='。请核对当前号码后再操作。';emit('refresh')}}}
 finally{if(active)busy.value=false}
}
onMounted(async()=>{priorFocus=document.activeElement as HTMLElement;tick=setInterval(()=>now.value=Date.now(),250);await nextTick();dialog.value?.querySelector<HTMLInputElement>('input')?.focus()})
onUnmounted(()=>{active=false;clearInterval(tick);if(priorFocus?.isConnected)priorFocus.focus()})
</script>
<template>
 <Teleport to="body"><div class="modal-overlay draw-edit-overlay" @click.self="close" @keydown.esc.stop="close" @keydown="trap">
  <section ref="dialog" class="modal draw-editor" role="dialog" aria-modal="true" aria-labelledby="draw-editor-title" tabindex="-1">
   <header><h2 id="draw-editor-title">修改当前采用号码</h2><button class="modal-close icon-button" :disabled="busy" aria-label="关闭号码修改" @click="close"><X :size="19"/></button><p>{{session.name}} · 第 {{session.issue}} 期</p><p>操作截止 {{datetime(new Date(cutoff).toISOString())}}</p></header>
   <form @submit.prevent="save">
    <div class="draw-edit-body">
     <div class="draw-original"><span>当前号码</span><strong>{{session.round.display_numbers.join(',')}}</strong></div>
     <p class="draw-edit-rule">共 {{rule.count}} 个号码，每位 {{rule.min}}–{{rule.max}}，{{rule.allow_repeat?'允许重复':'不可重复'}}<template v-if="rule.count===7&&rule.max===49">；第7位为特别号</template><template v-if="rule.count===8&&rule.max===45">；第7、8位为附加号1、2</template>。</p>
     <div class="draw-inputs"><label v-for="(_,i) in inputs" :key="i">{{rule.count===7&&rule.max===49&&i===6?'特别号':rule.count===8&&rule.max===45&&i>=6?`附加号${i-5}`:`第${i+1}位`}}<input v-model="inputs[i]" :aria-label="`第${i+1}位号码`" inputmode="numeric" maxlength="2" autocomplete="off" :disabled="busy||locked" @input="error=''"/></label></div>
     <p class="draw-edit-scope">保存后修改该场次本期所有用户共用的实际号码，并重新计算预计盈亏。</p>
     <p v-if="locked" class="form-error" role="alert">已到操作截止时间，不可修改。请关闭弹窗并刷新查看新一期。</p>
     <p v-else-if="error" class="form-error" role="alert">{{error}}</p>
    </div>
    <footer><button type="button" class="secondary-button" :disabled="busy" @click="close">取消</button><button class="primary-button" :disabled="busy||locked||unchanged">{{busy?'正在保存…':'保存并采用'}}</button></footer>
   </form>
  </section>
 </div></Teleport>
</template>
<style scoped>
.draw-edit-overlay{padding:16px}.draw-editor{display:flex;flex-direction:column;width:640px;max-width:100%;max-height:calc(100dvh - 32px);padding:0;overflow:hidden;border-radius:14px;text-align:left}.draw-editor header{padding:18px 20px 12px;flex-shrink:0;border-bottom:1px solid #eee8f3}.draw-editor h2{font-size:18px;line-height:1.4;margin:0 25px 8px 0}.draw-editor header p{font-size:11px;margin:4px 0;line-height:1.5}.draw-editor form{display:flex;flex-direction:column;min-height:0}.draw-edit-body{padding:16px 20px;overflow:auto;min-height:0}.draw-original{display:flex;gap:12px;align-items:center;flex-wrap:wrap;padding:10px 12px;background:#f8f5fc;border-radius:8px;font-size:12px;color:#94829f}.draw-original strong{color:#5e536f;overflow-wrap:anywhere;font-variant-numeric:tabular-nums}.draw-editor .draw-edit-rule,.draw-editor .draw-edit-scope{font-size:11px;line-height:1.6;margin:12px 0;color:#9583a2}.draw-editor .draw-edit-scope{margin-bottom:0;color:#a27e43}.draw-inputs{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:12px}.draw-inputs label{display:flex;flex-direction:column;gap:6px;font-size:11px;color:#8e7c9d}.draw-inputs input{width:100%;min-width:0;padding:10px 6px;border:1px solid #ddd3e8;border-radius:7px;background:#fff;color:#625172;text-align:center;font-size:18px;font-weight:600;font-variant-numeric:tabular-nums;outline:none}.draw-inputs input:focus{border-color:#9873c7;box-shadow:0 0 0 3px #9573c715}.draw-editor footer{display:flex;justify-content:flex-end;gap:8px;padding:12px 20px;border-top:1px solid #eee8f3;flex-shrink:0}.draw-editor footer button{font-size:12px;padding:9px 15px}.draw-editor .form-error{font-size:12px;padding:8px 10px;line-height:1.5;color:#bf4e57;margin:12px 0 0}.draw-editor button:disabled,.draw-editor input:disabled{opacity:.5;cursor:not-allowed}@media(max-width:480px){.draw-inputs{grid-template-columns:repeat(3,minmax(0,1fr));gap:10px}.draw-editor header{padding:16px 16px 10px}.draw-edit-body{padding:12px 16px}.draw-editor footer{padding:10px 16px}}
</style>
