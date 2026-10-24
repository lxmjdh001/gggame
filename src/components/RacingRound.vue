<script setup lang="ts">
import { formatIssue } from '../lib/issueDisplay'
import { computed } from 'vue'
import RacingResult from './RacingResult.vue'
import { ChevronRight, Clock3 } from 'lucide-vue-next'
import type { State } from '../api/games'
import { roundStatusLabels, type RoundPhase } from '../lib/roundStatus'
const props = defineProps<{ state: State; phase: RoundPhase; now: number; busy: boolean; historyOpen: boolean }>()
const emit = defineEmits<{ history: [] }>()
const issue = computed(() => props.state.next_draw_at.slice(0,16).replace(/-/g,'').replace('T','').replace(':',''))
const unavailable = computed(() => props.phase === 'syncing' || props.phase === 'paused')
function countdown(target: number) {
 if (unavailable.value || !Number.isFinite(target) || !Number.isFinite(props.now)) return '--:--:--'
 const seconds = Math.max(0, Math.ceil((target - props.now) / 1000))
 return [Math.floor(seconds / 3600), Math.floor(seconds % 3600 / 60), seconds % 60].map(n => String(n).padStart(2, '0')).join(':')
}
const closeClock = computed(() => countdown(props.phase === 'open' ? Date.parse(props.state.bet_close_at) : 0))
// Match the displayed :00 draw time; the server still controls publication and settlement.
const drawClock = computed(() => countdown(Math.floor(Date.parse(props.state.next_draw_at) / 60000) * 60000))
</script>
<template>
 <section class="racing-round" aria-label="北京PK10开奖结果与倒计时">
  <div class="racing-countdown">
   <Clock3 class="racing-clock-icon" :size="18"/>
   <span class="racing-current">{{ formatIssue(issue) }}<small>期</small></span>
   <span class="racing-timer" :aria-label="`封盘倒计时 ${closeClock}`">封盘: <b>{{ closeClock }}</b></span>
   <span class="racing-timer" :aria-label="`开奖倒计时 ${drawClock}`">开奖: <b>{{ drawClock }}</b></span>
  </div>
  <p v-if="phase !== 'open'" class="racing-phase" role="status">{{ roundStatusLabels[phase] }}</p>
  <button class="racing-latest" :disabled="busy" :aria-expanded="historyOpen" aria-label="查看北京PK10开奖记录" @click="emit('history')">
   <div class="racing-result-caption"><span v-if="state.latest">第 {{formatIssue(state.latest.issue)}} 期 开奖结果</span><span v-else>等待开奖结果</span><ChevronRight :size="12"/></div>
   <RacingResult v-if="state.latest" :numbers="state.latest.numbers"/>
  </button>
 </section>
</template>
<style scoped>
.racing-round{background:#fff;color:#43434f;border-bottom:1px solid #e9e6ed}
.racing-countdown{display:flex;align-items:center;gap:14px;min-height:43px;padding:9px 15px;border-bottom:1px solid #efedf3;font-size:12px;white-space:nowrap}
.racing-clock-icon{flex-shrink:0;color:#9396a3}
.racing-current,.racing-timer b{font-variant-numeric:tabular-nums}
.racing-current small{font-size:inherit;margin-left:3px}
.racing-timer{white-space:nowrap}
.racing-timer b{color:#b46e83;font-weight:500}
.racing-phase{padding:6px 15px;background:#faf8fc;font-size:11px;color:#a07988}
.racing-latest{display:block;width:100%;min-width:0;text-align:center;padding:11px 15px}
.racing-result-caption{display:flex;align-items:center;justify-content:center;gap:4px;font-size:12px;margin-bottom:8px}
.racing-result-caption svg{flex-shrink:0;transition:transform .15s}
.racing-latest[aria-expanded="true"] .racing-result-caption svg{transform:rotate(90deg)}
@media(max-width:760px){.racing-countdown{gap:8px;padding:8px;min-height:37px;font-size:10px}.racing-clock-icon{width:16px;height:16px}.racing-timer{font-size:10px}.racing-phase{padding:5px 8px;font-size:10px}.racing-latest{padding:9px 8px}.racing-result-caption{font-size:10px;margin-bottom:6px}}
@media(max-width:360px){.racing-countdown{gap:5px;padding:8px 6px;font-size:9px}.racing-current small{margin-left:1px}.racing-timer{font-size:9px}.racing-latest{padding:9px 6px}}
</style>

<style scoped>
.racing-round{font-family:Arial,'Microsoft YaHei','PingFang SC',sans-serif;color:#25262b}
.racing-countdown{gap:10px;padding:11px 12px;font-size:18px;font-weight:500}
.racing-current{font-size:22px;font-weight:600}.racing-timer b{color:#a44760;font-weight:600}
.racing-result-caption{font-size:20px;font-weight:600;margin-bottom:10px}
.racing-result-caption svg{width:17px;height:17px}
@media(max-width:760px){
 .racing-countdown{gap:5px;padding:11px 8px;min-height:48px;font-size:15px}
 .racing-current{font-size:19px}.racing-timer{font-size:15px;letter-spacing:-.3px}
 .racing-clock-icon{width:18px;height:18px}
 .racing-result-caption{font-size:18px;margin-bottom:9px}
 .racing-latest{padding:12px 8px}
}
@media(max-width:360px){
 .racing-countdown{flex-wrap:wrap;gap:8px 10px;padding:10px 8px}
 .racing-current{font-size:19px;flex-basis:calc(100% - 28px)}
 .racing-timer{font-size:16px;letter-spacing:0}
 .racing-result-caption{font-size:17px}
 .racing-latest{padding:12px 6px}
}
</style>
