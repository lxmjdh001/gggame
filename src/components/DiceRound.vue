<script setup lang="ts">
import { formatIssue } from '../lib/issueDisplay'
import { computed } from 'vue'
import { Bell, ChevronRight, Clock3 } from 'lucide-vue-next'
import type { State } from '../api/games'
import { roundStatusLabels, type RoundPhase } from '../lib/roundStatus'
import DiceResult from './DiceResult.vue'

const props = defineProps<{ state: State; phase: RoundPhase; now: number; busy: boolean; historyOpen: boolean }>()
const emit = defineEmits<{ history: [] }>()
const issue = computed(() => props.state.next_draw_at.slice(0, 16).replaceAll('-', '').replace('T', '').replace(':', ''))
const unavailable = computed(() => props.phase === 'syncing' || props.phase === 'paused')
const numbers = computed(() => props.state.latest?.numbers ?? [])

function countdown(target: number) {
 if (unavailable.value || !Number.isFinite(target)) return '--:--:--'
 const seconds = Math.max(0, Math.ceil((target - props.now) / 1000))
 return [Math.floor(seconds / 3600), Math.floor(seconds % 3600 / 60), seconds % 60].map(number => String(number).padStart(2, '0')).join(':')
}
const closeClock = computed(() => countdown(props.phase === 'open' ? Date.parse(props.state.bet_close_at) : 0))
// Displayed draw time is on the minute; publication and settlement keep the server schedule.
const drawClock = computed(() => countdown(Math.floor(Date.parse(props.state.next_draw_at) / 60000) * 60000))
</script>

<template>
 <section class="dice-round" aria-label="快三开奖结果与倒计时">
  <div class="dice-countdown">
   <Clock3 class="dice-clock-icon" :size="18"/>
   <span class="dice-current">{{ formatIssue(issue) }}<small>期</small></span>
   <span class="dice-timer">封盘: <b>{{ closeClock }}</b></span>
   <span class="dice-timer">开奖: <b>{{ drawClock }}</b></span>
  </div>
  <p v-if="phase !== 'open'" class="dice-phase" role="status">{{ roundStatusLabels[phase] }}</p>
  <button class="dice-latest" :disabled="busy" :aria-expanded="historyOpen" aria-label="查看快三开奖记录" @click="emit('history')">
   <Bell class="dice-bell" :size="18"/>
   <span v-if="state.latest" class="dice-issue" :title="state.latest.issue"><strong>{{ formatIssue(state.latest.issue) }}<em>期</em></strong></span>
   <span v-else class="dice-wait">等待开奖结果</span>
   <DiceResult :numbers="numbers"/>
   <span class="dice-history-label">开奖记录</span><ChevronRight class="dice-expand" :class="{ expanded: historyOpen }" :size="17"/>
  </button>
 </section>
</template>

<style scoped>
.dice-round{background:#fff;color:#303135;border-bottom:1px solid #e9e6ed;font-family:Arial,'Microsoft YaHei','PingFang SC',sans-serif}
.dice-countdown{display:flex;align-items:center;gap:12px;min-height:49px;padding:10px 15px;border-bottom:1px solid #efedf3;font-size:15px;white-space:nowrap}
.dice-clock-icon,.dice-bell,.dice-expand{flex-shrink:0;color:#9396a3}.dice-clock-icon,.dice-bell{width:23px;height:23px}
.dice-current,.dice-timer b,.dice-issue{font-variant-numeric:tabular-nums}.dice-current small{font-size:inherit;margin-left:3px}.dice-timer{white-space:nowrap}.dice-timer b{color:#ad6578;font-weight:400}
.dice-phase{padding:6px 15px;background:#faf8fc;font-size:13px;color:#a07988}.dice-latest{display:flex;align-items:center;gap:13px;width:100%;padding:13px 15px;min-height:80px;text-align:left}
.dice-issue{display:flex;flex-direction:column;gap:3px;white-space:nowrap;flex-shrink:0}.dice-issue small{font-size:12px;color:#89838f;letter-spacing:.2px}.dice-issue strong{font-size:19px;font-weight:500}.dice-issue em{font-style:normal;font-size:14px;margin-left:3px;color:#78727f}
.dice-history-label{margin-left:auto;font-size:14px;color:#9b8ba8;white-space:nowrap}.dice-expand{transition:transform .15s}.dice-expand.expanded{transform:rotate(90deg)}.dice-wait{font-size:14px;color:#96909c}
@media(max-width:760px){.dice-countdown{gap:7px;padding:9px 10px;min-height:43px;font-size:13px}.dice-clock-icon{width:19px;height:19px}.dice-timer{font-size:13px}.dice-phase{padding:6px 10px;font-size:12px}.dice-latest{padding:12px 10px;gap:8px;min-height:73px}.dice-bell{width:20px;height:20px}.dice-issue small{font-size:10px}.dice-issue strong{font-size:17px}.dice-issue em{font-size:12px;margin-left:2px}.dice-history-label{display:none}.dice-expand{margin-left:auto;width:15px;height:15px}}
@media(max-width:360px){.dice-countdown{gap:5px;padding:9px 6px;font-size:11px}.dice-clock-icon{width:17px;height:17px}.dice-current small{margin-left:1px}.dice-timer{font-size:11px}.dice-latest{padding:11px 6px;gap:5px}.dice-issue small{font-size:9px}.dice-issue strong{font-size:16px}.dice-bell{width:18px;height:18px}}
@media(prefers-reduced-motion:reduce){.dice-expand{transition:none}}
</style>

<style scoped>
.dice-countdown{font-size:18px;font-weight:500;color:#25262b;gap:10px;padding:11px 12px}
.dice-current{font-size:22px;font-weight:600}.dice-timer b{font-weight:600;color:#a44760}
.dice-issue strong{font-size:23px;font-weight:600}.dice-issue em{font-size:16px;color:#4b4750}
.dice-latest :deep(.dice-result){--dice-face-size:36px;gap:7px}
.dice-latest :deep(.dice-result-tag){font-size:23px;min-width:27px;font-weight:500}
.dice-latest :deep(.dice-equals){font-size:22px;font-weight:500}
.dice-latest :deep(.dice-sum){font-size:25px;font-weight:700}
@media(max-width:760px){
 .dice-countdown{gap:5px;padding:11px 8px;font-size:15px;min-height:48px}
 .dice-current{font-size:19px}.dice-timer{font-size:15px;letter-spacing:-.3px}
 .dice-clock-icon{width:18px;height:18px}
 .dice-latest{gap:6px;padding:14px 8px;min-height:82px}
 .dice-issue strong{font-size:21px}.dice-issue em{font-size:14px}
 .dice-latest :deep(.dice-result){--dice-face-size:32px;gap:4px}
 .dice-latest :deep(.dice-result-tag){font-size:20px;min-width:22px}
 .dice-latest :deep(.dice-equals){font-size:19px}
 .dice-latest :deep(.dice-sum){font-size:23px;min-width:26px}
}
@media(max-width:360px){
 .dice-countdown{flex-wrap:wrap;gap:8px 10px;padding:10px 8px}
 .dice-current{font-size:19px;flex-basis:calc(100% - 28px)}
 .dice-timer{font-size:16px;letter-spacing:0}
 .dice-latest{gap:4px;padding:14px 6px}
 .dice-issue strong{font-size:19px}.dice-issue em{font-size:12px}
 .dice-bell{width:16px;height:18px}
 .dice-latest :deep(.dice-result){--dice-face-size:27px;gap:3px}
 .dice-latest :deep(.dice-result-tag){font-size:18px;min-width:18px}
 .dice-latest :deep(.dice-equals){font-size:17px}
 .dice-latest :deep(.dice-sum){font-size:21px;min-width:23px}
 .dice-expand{width:13px}
}
</style>
