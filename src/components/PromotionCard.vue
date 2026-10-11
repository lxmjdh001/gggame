<script setup lang="ts">
import {computed} from 'vue'
import {Gift,Heart,Sparkles,Ticket,Trophy,PartyPopper,ArrowUpRight} from 'lucide-vue-next'
import {themes,type PromotionInput} from '../api/promotions'
const props=defineProps<{promotion:PromotionInput;preview?:boolean}>()
defineEmits<{activate:[]}>()
const theme=computed(()=>themes.find(t=>t.id===props.promotion.theme)??themes[0]!)
const icon=computed(()=>({gift:Gift,heart:Heart,sparkles:Sparkles,ticket:Ticket,trophy:Trophy,party:PartyPopper}[props.promotion.icon]??Gift))
</script>
<template><article class="promotion-cover" :style="{'--cover-bg':theme.bg,'--cover-end':theme.end,'--cover-ink':theme.ink,'--cover-line':theme.line}"><component :is="icon" :size="46"/><span>{{promotion.tagline||'专属福利'}}</span><h2>{{promotion.title||'活动标题'}}</h2><p>{{promotion.summary||'在这里填写活动简介，封面将自动生成。'}}</p><button class="cover-button" :disabled="preview" @click="$emit('activate')">{{promotion.button||'查看详情'}}<ArrowUpRight :size="16"/></button></article></template>
<style scoped>
.promotion-cover{background:linear-gradient(125deg,var(--cover-bg),var(--cover-end));color:var(--cover-ink);border:1px solid var(--cover-line);border-radius:18px;padding:36px;display:flex;flex-direction:column;align-items:flex-start;min-width:0;overflow-wrap:anywhere}.promotion-cover>span{font-size:12px;margin-top:30px;font-weight:600}.promotion-cover h2{font-size:27px;margin:14px 0 0;line-height:1.5}.promotion-cover p{font-size:13px;line-height:2;white-space:pre-line;margin:22px 0 30px;opacity:.8}.cover-button{display:flex;gap:12px;align-items:center;margin-top:auto;border:1px solid var(--cover-line);padding:13px 18px;background:#ffffffd9;border-radius:9px;color:var(--cover-ink);font-size:12px;font-weight:600}.cover-button:disabled{opacity:1}.promotion-cover>svg{stroke-width:1.9}@media(max-width:760px){.promotion-cover{padding:26px}.promotion-cover h2{font-size:23px}.promotion-cover>span{margin-top:22px}.promotion-cover p{font-size:12px}}
</style>
