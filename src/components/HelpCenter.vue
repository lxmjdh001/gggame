<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue'
import {Search,RefreshCw} from 'lucide-vue-next'
import {api} from '../api/account'
import type {HelpArticle} from '../api/help'
const items=ref<HelpArticle[]>([]),query=ref(''),loading=ref(true),error=ref('')
const matches=computed(()=>items.value.filter(p=>(p.title+'\n'+p.content).toLowerCase().includes(query.value.trim().toLowerCase())))
let active=true,pending=false,timer:ReturnType<typeof setInterval>|undefined
async function load(){if(pending)return;pending=true;try{const next=await api<HelpArticle[]>('/help-articles');if(active){items.value=next;error.value=''}}catch(e){if(active)error.value=(e as Error).message}finally{pending=false;if(active)loading.value=false}}
onMounted(()=>{void load();timer=setInterval(()=>void load(),15000)})
onUnmounted(()=>{active=false;clearInterval(timer)})
</script>
<template><div class="help-content"><label class="help-search"><Search :size="16"/><input v-model="query" placeholder="搜索问题或关键词" aria-label="搜索帮助内容"/></label><p v-if="error" class="form-error" role="alert">{{error}} <button class="text-button" @click="load"><RefreshCw :size="14"/>重试</button></p><p v-if="loading" role="status">正在加载帮助内容…</p><template v-else><details v-for="p in matches" :key="p.id"><summary>{{p.title}}</summary><p class="help-answer">{{p.content}}</p></details><p v-if="!matches.length&&!error" class="empty-copy">{{query.trim()?'未找到相关内容，请换个关键词。':'暂无帮助内容。'}}</p></template></div></template>
<style scoped>
.help-content{text-align:left}.help-search{display:flex;align-items:center;gap:9px;border:1px solid #e5deef;border-radius:9px;padding:10px 12px;margin:18px 0;color:#9988af}.help-search input{width:100%;min-width:0;outline:none;border:0;background:transparent;font-size:13px;color:#554b66}.help-content details{overflow-wrap:anywhere}.help-content .help-answer{white-space:pre-wrap;line-height:1.9;font-size:13px}.help-content .text-button{display:inline-flex;gap:4px;align-items:center}
</style>
