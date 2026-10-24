<script setup lang="ts">
import {ref,onUnmounted,watch} from 'vue'
import {Search,RefreshCw,GitBranch} from 'lucide-vue-next'
import {user} from '../api/account'
import {loadAgents,type AgentPage} from '../api/agents'
import AdminAgentNode from './AdminAgentNode.vue'
const query=ref(''),selected=ref(''),data=ref<AgentPage|null>(null),busy=ref(false),error=ref(''),revision=ref(0),compact=ref(false)
let generation=0
async function load(page=1){
 const ticket=++generation,id=user.value?.id
 data.value=null;busy.value=true;error.value='';revision.value++
 if(user.value?.role!=='admin'){busy.value=false;return}
 try{const next=await loadAgents({username:selected.value,page});if(ticket===generation&&user.value?.id===id)data.value=next}
 catch(e){if(ticket===generation)error.value=e instanceof Error?e.message:'代理关系读取失败'}
 finally{if(ticket===generation)busy.value=false}
}
function focus(username:string){query.value=username;selected.value=username;void load()}
function reset(){query.value='';selected.value='';void load()}
watch(()=>user.value?.id,()=>{void load()},{immediate:true})
onUnmounted(()=>generation++)
</script>
<template>
 <section class="surface admin-agents">
  <header class="agent-toolbar"><div><h2><GitBranch :size="22"/>代理关系</h2><p>按邀请关系逐层展开，不限于佣金的三级范围。</p></div><button class="secondary-button" :disabled="busy" @click="load(data?.page??1)"><RefreshCw :size="15"/>刷新关系</button></header>
  <form class="agent-search" @submit.prevent="focus(query.trim())"><label><Search :size="16"/><input v-model="query" maxlength="32" placeholder="输入完整会员账号" aria-label="查询代理账号"/></label><button class="primary-button" :disabled="busy">查询关系</button><button type="button" class="secondary-button" :disabled="busy" @click="reset">全部顶层</button></form>
  <p class="agent-definition">充值口径：历史累计已审核充值＋正数后台充值（含明确标注充值的旧调账），不含奖励、待审核或驳回充值，不抵扣管理扣减，不随提现归零；下级数据不含本人，包含已停用会员。仅读取资料，不修改上级或资金。</p>
  <div class="agent-controls"><strong>{{selected?'当前根账号：'+selected:'全部顶层账号'}}<small v-if="data"> · 共{{data.total}}个</small></strong><label><input v-model="compact" type="checkbox"/>紧凑显示</label><span>可横向滚动查看展开的关系</span></div>
  <p v-if="busy" role="status">正在读取代理关系…</p>
  <div v-if="error" class="form-error" role="alert">{{error}} <button class="secondary-button" @click="load()">重新读取</button></div>
  <div v-if="data" class="agent-canvas" :class="{compact}" tabindex="0" role="region" aria-label="代理邀请关系图（可横向滚动）">
   <ul class="agent-roots"><AdminAgentNode v-for="(node,index) in data.items" :key="revision+':'+node.id" :node="node" :initial-open="index===0" @focus="focus"/></ul>
   <p v-if="!data.items.length" class="empty-copy">暂无代理邀请关系</p>
  </div>
  <nav v-if="data&&data.total>data.page_size" class="agent-pagination" aria-label="顶层代理分页"><span>第{{data.page}} / {{Math.ceil(data.total/data.page_size)}}页，每页{{data.page_size}}个账号</span><button :disabled="busy||data.page<=1" @click="load(data.page-1)">上一页</button><button :disabled="busy||data.page*data.page_size>=data.total" @click="load(data.page+1)">下一页</button></nav>
 </section>
</template>
<style scoped>
.agent-search input{box-sizing:border-box;padding:11px 12px;border:1px solid #e1d8ec;border-radius:7px;background:#faf8fd;color:#51405d;outline:none}.agent-search input:focus{border-color:#9b78cd;box-shadow:0 0 0 3px #9b78cd15}
.admin-agents{min-width:0}.agent-toolbar,.agent-controls,.agent-search,.agent-pagination{display:flex;align-items:center;flex-wrap:wrap;gap:12px}.agent-toolbar{justify-content:space-between}.agent-toolbar h2{display:flex;align-items:center;gap:8px;font-size:20px}.agent-toolbar p,.agent-definition{color:#97869f;font-size:12px;line-height:1.8}.agent-search{margin:22px 0 14px}.agent-search label{display:flex;align-items:center;gap:8px;flex:1;min-width:180px}.agent-search input{width:100%;min-width:0}.agent-definition{padding:12px;background:#f8f5fc;border-radius:7px}.agent-controls{font-size:12px;color:#8a7696;padding:16px 0}.agent-controls strong{color:#685270;overflow-wrap:anywhere}.agent-controls small{font-weight:400}.agent-controls label{display:flex;align-items:center;gap:5px;margin-left:auto}.agent-controls input{width:auto}.agent-canvas{max-width:100%;overflow:auto;max-height:75vh;background:#fbf8ff;border:1px solid #eee5f5;border-radius:9px;overscroll-behavior:contain}.agent-canvas:focus-visible{outline:2px solid #9573cc}.agent-roots{display:flex;flex-direction:column;gap:30px;margin:0;padding:24px;width:max-content;min-width:100%;box-sizing:border-box}.agent-canvas.compact{--agent-width:235px;--agent-font:11px}.agent-pagination{justify-content:flex-end;margin-top:16px;font-size:12px;color:#8a7696}.agent-pagination button{padding:8px;border:1px solid #e2d6ef;border-radius:5px}.admin-agents button:disabled{opacity:.5;cursor:not-allowed}@media(max-width:760px){.agent-search input{font-size:16px}.agent-roots{padding:14px}.agent-controls label{margin-left:0}.agent-toolbar h2{font-size:18px}}
</style>
