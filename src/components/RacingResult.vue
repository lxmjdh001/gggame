<script setup lang="ts">
import { computed } from 'vue'
const props = defineProps<{ numbers: number[] }>()
const summary = computed(() => {
 const numbers = props.numbers
 if (!numbers || numbers.length !== 10) return []
 const sum = numbers[0]! + numbers[1]!
 return [{label:'冠亚和',value:String(sum)}, {label:'和大小',value:sum<=11?'小':'大'}, {label:'和单双',value:sum%2?'单':'双'}, {label:'龙虎（第1名与第10名）',value:numbers[0]!>numbers[9]!?'龙':'虎'}]
})
</script>
<template>
 <div class="racing-result">
  <div class="racing-balls"><span v-for="(number,index) in numbers" :key="index" class="racing-ball" :class="`racing-ball-${number}`">{{String(number).padStart(2,'0')}}</span></div>
  <div v-if="summary.length" class="racing-result-summary"><span v-for="item in summary" :key="item.label" :aria-label="`${item.label}：${item.value}`"><small>{{item.label==='冠亚和'?'和':item.label.startsWith('龙虎')?'龙虎':''}}</small>{{item.value}}</span></div>
 </div>
</template>
<style scoped>
.racing-result{width:100%;min-width:0;font-family:Arial,'Microsoft YaHei','PingFang SC',sans-serif}
.racing-balls{display:grid;grid-template-columns:repeat(10,minmax(0,1fr));gap:4px;max-width:650px;margin:0 auto}
.racing-ball{display:grid;place-items:center;height:36px;border-radius:3px;color:white;font-weight:750;font-size:20px;text-shadow:0 1px 2px #0005;box-shadow:inset 0 -2px 0 #0001}
.racing-ball-1{background:#ebc600;color:#303941;text-shadow:none}.racing-ball-2{background:#157ac7}.racing-ball-3{background:#28303a}.racing-ball-4{background:#f37322}.racing-ball-5{background:#00b9d4}.racing-ball-6{background:#8b43cc}.racing-ball-7{background:#9b9fa2}.racing-ball-8{background:#ec424e}.racing-ball-9{background:#99752e}.racing-ball-10{background:#36944d}
.racing-result-summary{display:flex;justify-content:center;gap:6px;margin-top:7px}
.racing-result-summary>span{display:flex;align-items:center;gap:4px;min-width:24px;height:28px;justify-content:center;padding:0 5px;border:1px solid #e4e5e9;border-radius:3px;color:#4b4750;font-size:16px}
.racing-result-summary small{font-size:12px;color:#78727f}
@media(max-width:760px){.racing-balls{gap:3px}.racing-ball{height:32px;font-size:18px;border-radius:2px}.racing-result-summary{gap:5px;margin-top:8px}.racing-result-summary>span{height:25px;font-size:14px;min-width:24px;padding:0 4px}.racing-result-summary small{font-size:11px}}
@media(max-width:360px){.racing-ball{height:29px;font-size:16px}.racing-balls{gap:2px}}
</style>
