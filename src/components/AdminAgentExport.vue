<script setup lang="ts">
import {ref,watch,onUnmounted} from 'vue'
import {Download} from 'lucide-vue-next'
import {user} from '../api/account'
import {exportAgents} from '../api/agents'
import {agentRelationImage,saveAgentExport,type AgentExportFile,type AgentExportSnapshot} from '../lib/agentExport'
const props=defineProps<{username:string;disabled:boolean}>()
const format=ref<'xlsx'|'image'>('xlsx'),busy=ref(false),error=ref(''),message=ref('')
let generation=0,controller:AbortController|undefined
function cancel(){generation++;controller?.abort();controller=undefined;busy.value=false;error.value='';message.value=''}
watch([()=>props.username,()=>props.disabled,()=>user.value?.id,()=>user.value?.role,format],cancel)
onUnmounted(cancel)
async function download(){
 if(props.disabled||busy.value||user.value?.role!=='admin')return
 const ticket=++generation,owner=user.value.id,scope=props.username,choice=format.value
 const abort=new AbortController();controller=abort;busy.value=true;error.value='';message.value=''
 const current=()=>ticket===generation&&!abort.signal.aborted&&user.value?.id===owner&&user.value?.role==='admin'&&props.username===scope&&!props.disabled
 try{
  const result=await exportAgents(scope,choice==='xlsx'?'xlsx':'data',abort.signal)
  if(!current())return
  const file=choice==='image'?await agentRelationImage(result as AgentExportSnapshot,abort.signal):result as AgentExportFile
  if(!current())return
  saveAgentExport(file);message.value=file.note??(choice==='image'?'完整关系图已导出为 PNG 图片。':'完整团队已导出为 Excel 表格。')
 }catch(e){if(current())error.value=e instanceof Error?e.message:'导出失败，请重试'}
 finally{if(ticket===generation){busy.value=false;controller=undefined}}
}
</script>
<template>
 <div class="agent-export">
  <div class="agent-export-actions">
   <label><span class="agent-export-label">导出格式</span><select v-model="format" :disabled="busy" aria-label="代理关系导出格式"><option value="xlsx">Excel 表格</option><option value="image">关系图图片</option></select></label>
   <button type="button" class="secondary-button" :disabled="disabled||busy||user?.role!=='admin'" :aria-busy="busy" :title="username?'导出 '+username+' 及全部下级':'导出全部团队，不受分页和展开状态影响'" @click="download"><Download :size="15"/>{{busy?'正在导出…':'导出'}}</button>
  </div>
  <p v-if="busy" class="agent-export-message" role="status">正在生成完整团队文件，请稍候…</p>
  <p v-else-if="message" class="agent-export-message" role="status">{{message}}</p>
  <p v-if="error" class="agent-export-error" role="alert">{{error}}，可点击导出重试。</p>
 </div>
</template>
<style scoped>
.agent-export{min-width:0;max-width:100%}.agent-export-actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.agent-export-actions label{display:flex;align-items:center;gap:7px;color:#8a7696;font-size:12px}.agent-export-actions select{max-width:155px;padding:9px 10px;border:1px solid #e1d8ec;border-radius:7px;background:#fff;color:#685270;font:inherit}.agent-export-actions select:focus-visible{outline:2px solid #9573cc;outline-offset:2px}.agent-export button:disabled,.agent-export select:disabled{opacity:.5;cursor:not-allowed}.agent-export-message,.agent-export-error{margin:5px 0 0;max-width:340px;font-size:11px;line-height:1.5;color:#8a7696;overflow-wrap:anywhere}.agent-export-error{color:#ad4465}@media(max-width:760px){.agent-export-label{display:none}.agent-export-actions{gap:6px}.agent-export-actions select{font-size:12px}.agent-export-actions button{font-size:11px;padding:9px}}
</style>
