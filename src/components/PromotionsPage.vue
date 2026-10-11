<script setup lang="ts">
import {onMounted,onUnmounted,ref} from 'vue'
import {X,Gift} from 'lucide-vue-next'
import {api} from '../api/account'
import type {Promotion} from '../api/promotions'
import PromotionCard from './PromotionCard.vue'
const emit=defineEmits<{navigate:[action:string]}>()
const items=ref<Promotion[]>([]),loading=ref(true),error=ref(''),selected=ref<Promotion|null>(null),dialog=ref<HTMLDialogElement|null>(null)
let timer:ReturnType<typeof setInterval>|undefined,active=true,pending=false,overflow:string|null=null
async function load(){if(pending)return;pending=true;try{const data=await api<Promotion[]>('/promotions');if(active){items.value=data;error.value='';if(selected.value&&!data.some(p=>p.id===selected.value!.id)){dialog.value?.close();restore()}}}catch(e){if(active)error.value=(e as Error).message}finally{pending=false;if(active)loading.value=false}}
function open(p:Promotion){if(p.action!=='details'){emit('navigate',p.action);return};selected.value=p;dialog.value?.showModal();overflow=document.body.style.overflow;document.body.style.overflow='hidden'}
function restore(){if(overflow!==null){document.body.style.overflow=overflow;overflow=null}}
function close(){dialog.value?.close();restore()}
onMounted(()=>{void load();timer=setInterval(()=>void load(),15000)});onUnmounted(()=>{active=false;clearInterval(timer);restore()})
</script>
<template><div class="page-heading"><div><p class="section-eyebrow">A LITTLE SOMETHING FOR YOU</p><h1>福利活动</h1><p>给日常一点惊喜，给热爱多一点回应。</p></div></div><div v-if="error" class="form-error" role="alert">{{error}} <button @click="load">重试</button></div><p v-if="loading" class="loading-line" role="status">正在加载活动…</p><div v-else-if="items.length" class="promotion-grid"><PromotionCard v-for="p in items" :key="p.id" :promotion="p" @activate="open(p)"/></div><div v-else-if="!error" class="empty-state"><Gift :size="40"/><h3>精彩活动即将上线</h3><p>敬请期待新的福利。</p></div><Teleport to="body"><dialog ref="dialog" class="promotion-detail" aria-labelledby="promotion-title" @close="restore"><header><h2 id="promotion-title">{{selected?.title}}</h2><button class="icon-button" aria-label="关闭活动详情" autofocus @click="close"><X/></button></header><div class="promotion-detail-body"><p class="promotion-detail-summary">{{selected?.summary}}</p><p>{{selected?.content||'更多活动信息敬请期待。'}}</p></div><footer><button class="primary-button" @click="close">知道了</button></footer></dialog></Teleport></template>
<style scoped>
.promotion-detail{padding:0;border:1px solid #ece5f7;border-radius:18px;background:#fff;color:#514b68;width:620px;max-width:calc(100vw - 32px);max-height:85dvh;overflow:hidden}.promotion-detail[open]{display:flex;flex-direction:column}.promotion-detail::backdrop{background:#30264470;backdrop-filter:blur(4px)}.promotion-detail header{display:flex;align-items:center;gap:16px;justify-content:space-between;padding:22px;border-bottom:1px solid #efedf6}.promotion-detail h2{font-size:22px;overflow-wrap:anywhere}.promotion-detail-body{overflow:auto;padding:24px;line-height:2;font-size:13px;white-space:pre-wrap;overflow-wrap:anywhere}.promotion-detail-summary{color:#9681ab;margin-bottom:18px}.promotion-detail footer{padding:16px 24px;border-top:1px solid #efedf6}.promotion-detail footer button{width:100%;justify-content:center}
</style>
