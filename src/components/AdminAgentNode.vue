<script setup lang="ts">
import {ref,onMounted,onUnmounted} from 'vue'
import {ChevronRight,ChevronDown,Users} from 'lucide-vue-next'
import {money,datetime} from '../api/account'
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
   <header><strong>{{node.username}}</strong><span>{{node.role==='admin'?'管理员':node.status==='disabled'?'已停用':'会员'}}</span></header>
   <p v-if="node.name" class="agent-name">{{node.name}}</p>
   <p>注册 {{datetime(node.created_at)}}</p>
   <p>上级：<button v-if="node.inviter_username" class="agent-link" @click="emit('focus',node.inviter_username)">{{node.inviter_username}}</button><span v-else>{{node.missing_parent?'上级记录缺失':'无上级'}}</span></p>
   <dl><div><dt>个人累计充值</dt><dd>{{money(node.deposit_cents)}} 元</dd></div><div><dt>直属 / 全部下级</dt><dd>{{node.direct_count}} / {{node.team_count}} 人</dd></div><div><dt>下级累计充值</dt><dd>{{money(node.team_deposit_cents)}} 元</dd></div></dl>
   <footer><button v-if="node.direct_count" :aria-expanded="expanded" :aria-label="`${expanded?'收起':'展开'} ${node.username} 的下级`" @click="toggle"><component :is="expanded?ChevronDown:ChevronRight" :size="14"/><Users :size="13"/>{{expanded?'收起下级':'展开下级'}}</button><span v-else>暂无下级</span><button class="agent-link" @click="emit('focus',node.username)">以此账号为根</button></footer>
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
.agent-branch{display:flex;align-items:center;position:relative;gap:32px;list-style:none}.agent-card{width:var(--agent-width,265px);flex-shrink:0;padding:16px;box-sizing:border-box;background:#fff;border:1px solid #dfd5f1;border-left:3px solid #8660cc;border-radius:10px;box-shadow:0 3px 12px #57317c08;font-size:var(--agent-font,12px)}.agent-card.disabled{border-left-color:#aaa;background:#fafafa}.agent-card header{display:flex;gap:10px;align-items:center;justify-content:space-between}.agent-card strong{font-size:1.2em;overflow-wrap:anywhere}.agent-card header span{font-size:.85em;color:#907d9e;background:#f4eefb;border-radius:4px;padding:3px 6px;white-space:nowrap}.agent-card p{margin:9px 0;color:#8a8095;font-size:.92em;line-height:1.6;overflow-wrap:anywhere}.agent-name{color:#584763!important}.agent-card dl{border-top:1px solid #f1edf6;margin:10px 0;padding-top:8px}.agent-card dl>div{display:flex;gap:8px;justify-content:space-between;line-height:1.9}.agent-card dt{color:#94859e}.agent-card dd{margin:0;font-variant-numeric:tabular-nums;white-space:nowrap}.agent-card footer{display:flex;justify-content:space-between;gap:8px;align-items:center;padding-top:6px;color:#a097a9;font-size:.9em}.agent-card footer button{display:flex;align-items:center;gap:3px}.agent-link{color:#7952b9;overflow-wrap:anywhere;text-decoration:underline;text-underline-offset:3px}.agent-children{position:relative;display:flex;flex-direction:column;gap:18px;padding:8px 0 8px 25px;margin:0;border-left:1px solid #c8b5df}.agent-children:before{content:'';position:absolute;left:-33px;top:50%;width:32px;border-top:1px solid #c8b5df}.agent-children>.agent-branch:before{content:'';position:absolute;left:-26px;top:50%;width:25px;border-top:1px solid #c8b5df}.branch-message{list-style:none;max-width:280px;padding:12px;font-size:12px;color:#9682a6}.branch-message button{color:#7952b9;text-decoration:underline}
</style>
