<script setup lang="ts">
import {ref,onMounted,onUnmounted} from 'vue'
import {ChevronRight,ChevronDown} from 'lucide-vue-next'
import {money} from '../api/account'
import {loadAgents,type AgentNode} from '../api/agents'
const props=defineProps<{node:AgentNode;initialOpen?:boolean}>()
const emit=defineEmits<{focus:[username:string]}>()
const expanded=ref(false),busy=ref(false),error=ref(''),children=ref<AgentNode[]>([]),page=ref(0),total=ref(props.node.direct_count)
let generation=0
async function load(){
 if(busy.value)return
 busy.value=true;error.value='';const ticket=generation
 try{const d=await loadAgents({parent_id:props.node.id,page:page.value+1});if(ticket!==generation)return;children.value=[...children.value,...d.items.filter(n=>!children.value.some(old=>old.id===n.id))];page.value=d.page;total.value=d.total}
 catch(e){if(ticket===generation)error.value=e instanceof Error?e.message:'读取下级失败'}
 finally{if(ticket===generation)busy.value=false}
}
function toggle(){expanded.value=!expanded.value;if(expanded.value&&page.value===0)void load()}
onMounted(()=>{if(props.initialOpen&&props.node.direct_count)toggle()})
onUnmounted(()=>generation++)
</script>
<template>
 <li class="agent-branch">
  <article class="agent-card" :class="{disabled:node.status==='disabled'}">
   <dl>
    <div><dt>用户名</dt><dd><button class="agent-account" :title="`以 ${node.username} 为根查看团队`" @click="emit('focus',node.username)">{{node.username}}</button></dd></div>
    <div><dt>上级</dt><dd><button v-if="node.inviter_username" class="agent-link" @click="emit('focus',node.inviter_username)">{{node.inviter_username}}</button><span v-else>{{node.missing_parent?'记录缺失':'无上级'}}</span></dd></div>
    <div><dt>个人累计充值</dt><dd>{{money(node.deposit_cents)}} 元</dd></div>
    <div><dt>直属 / 全部下级</dt><dd>{{node.direct_count}} / {{node.team_count}} 人</dd></div>
    <div><dt>下级累计充值</dt><dd>{{money(node.team_deposit_cents)}} 元</dd></div>
   </dl>
   <button v-if="node.direct_count" class="agent-expand" :aria-expanded="expanded" :aria-label="`${expanded?'收起':'展开'} ${node.username} 的下级`" @click="toggle"><component :is="expanded?ChevronDown:ChevronRight" :size="12"/>{{expanded?'收起':'展开'}} {{node.direct_count}}</button>
  </article>
  <ul v-if="expanded" class="agent-children" :aria-label="`${node.username} 的直属下级`">
   <AdminAgentNode v-for="child in children" :key="child.id" :node="child" @focus="emit('focus',$event)"/>
   <li v-if="busy" class="branch-message" role="status">正在读取下级…</li>
   <li v-if="error" class="branch-message" role="alert">{{error}} <button @click="load">重试</button></li>
   <li v-else-if="!busy&&children.length<total" class="branch-message"><button @click="load">加载更多下级（已显示{{children.length}} / {{total}}）</button></li>
   <li v-if="!busy&&!error&&!total" class="branch-message">暂无直属下级</li>
  </ul>
 </li>
</template>
<style scoped>
.agent-branch{display:flex;align-items:center;position:relative;gap:22px;list-style:none}.agent-card{width:var(--agent-width,236px);flex-shrink:0;padding:10px 11px;box-sizing:border-box;background:#fff;border:1px solid #dfd5f1;border-left:3px solid #8660cc;border-radius:6px;font-size:var(--agent-font,12px)}.agent-card.disabled{border-left-color:#aaa;background:#fafafa}.agent-card dl{margin:0}.agent-card dl>div{display:flex;gap:7px;justify-content:space-between;line-height:1.85}.agent-card dt{color:#94859e;flex-shrink:0}.agent-card dd{margin:0;min-width:0;text-align:right;overflow-wrap:anywhere;font-variant-numeric:tabular-nums}.agent-account{font-weight:600;color:#574168}.agent-link{color:#7952b9;text-decoration:underline;text-underline-offset:2px}.agent-expand{display:flex;align-items:center;gap:2px;margin:5px 0 0 auto;color:#8e72a7;font-size:10px}.agent-children{position:relative;display:flex;flex-direction:column;gap:10px;padding:4px 0 4px 15px;margin:0;border-left:1px solid #c8b5df}.agent-children:before{content:'';position:absolute;left:-23px;top:50%;width:22px;border-top:1px solid #c8b5df}.agent-children>.agent-branch:before{content:'';position:absolute;left:-16px;top:50%;width:15px;border-top:1px solid #c8b5df}.branch-message{list-style:none;max-width:250px;padding:8px;font-size:11px;color:#9682a6}.branch-message button{color:#7952b9;text-decoration:underline}
</style>
