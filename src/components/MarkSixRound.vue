<script setup lang="ts">
import { formatIssue } from '../lib/issueDisplay'
import { computed } from 'vue'
import { Bell, ChevronRight, Clock3, RefreshCw } from 'lucide-vue-next'
import type { State } from '../api/games'
import { roundStatusLabels, type RoundPhase } from '../lib/roundStatus'
import MarkSixBall from './MarkSixBall.vue'
import { markSixLunarYear, markSixZodiacLabel } from '../lib/marksixZodiac'
const props = defineProps<{ state: State; phase: RoundPhase; now: number; busy: boolean; historyOpen: boolean; refreshing?: boolean }>()
const emit = defineEmits<{ history: []; refresh: [] }>()
const issue = computed(() => props.state.next_draw_at.slice(0,16).replaceAll('-','').replace('T','').replace(':',''))
const legacyAustralia = computed(() => props.state.session.id==='lhc-au' && props.state.latest?.numbers.length===8)
const unavailable = computed(() => ['syncing','paused'].includes(props.phase))
function countdown(target:number) {
 if (unavailable.value || !Number.isFinite(target)) return '--:--:--'
 const seconds=Math.max(0,Math.ceil((target-props.now)/1000))
 return [Math.floor(seconds/3600),Math.floor(seconds%3600/60),seconds%60].map(n=>String(n).padStart(2,'0')).join(':')
}
const closeClock=computed(()=>countdown(props.phase==='open'?Date.parse(props.state.bet_close_at):0))
// The displayed draw starts on the minute; actual publication and settlement still use the server schedule.
const drawClock=computed(()=>countdown(Math.floor(Date.parse(props.state.next_draw_at)/60000)*60000))
const lunarYear=computed(()=>props.state.latest ? markSixLunarYear(props.state.latest.draw_at) : null)
function label(number:number,index:number){
 if(legacyAustralia.value)return index<6?`正${index+1}`:`附${index-5}`
 return markSixZodiacLabel(number,lunarYear.value)
}
function refresh(){if(!props.busy&&!props.refreshing)emit('refresh')}
function showHistory(){if(!props.busy)emit('history')}
</script>
<template>
 <section class="marksix-round" aria-label="六合彩开奖结果与倒计时">
  <div class="marksix-countdown">
   <Clock3 class="marksix-clock" :size="28"/><span class="marksix-current" :title="`第${issue}期`">{{formatIssue(issue)}}<small>期</small></span>
   <span class="marksix-timer">封盘: <b>{{closeClock}}</b></span><span class="marksix-timer">开奖: <b>{{drawClock}}</b></span>
   <button class="marksix-refresh" :disabled="busy||refreshing" :aria-busy="refreshing" aria-label="刷新六合彩余额与开奖" @click="refresh"><RefreshCw :size="19" :class="{refreshing}"/></button>
   <span v-if="phase!=='open'" class="marksix-phase" role="status">{{roundStatusLabels[phase]}}</span>
  </div>
  <button class="marksix-latest" :class="{'marksix-legacy-result':legacyAustralia}" :disabled="busy" :aria-expanded="historyOpen" aria-label="查看六合彩开奖记录" @click="showHistory">
   <Bell class="marksix-bell" :size="28"/>
   <span class="marksix-issue" v-if="state.latest" :title="`第${state.latest.issue}期`">{{formatIssue(state.latest.issue)}}<small>期</small></span><span v-else class="marksix-wait">等待开奖结果</span>
   <span v-if="state.latest" class="marksix-chips">
    <template v-for="(number,index) in state.latest.numbers" :key="index">
     <span v-if="index===6" class="marksix-plus">+</span>
     <span class="marksix-result" :aria-label="`${index<6?'正码'+(index+1):legacyAustralia?'附加号'+(index-5):'特码'} ${number} ${label(number,index)}`"><MarkSixBall :number="number"/><small>{{label(number,index)}}</small></span>
    </template>
   </span>
   <ChevronRight class="marksix-expand" :size="17" :class="{expanded:historyOpen}"/>
  </button>
 </section>
</template>
<style scoped>
.marksix-round{background:#fff;color:#23272a;border-bottom:1px solid #e3e3e3;min-width:0}
.marksix-countdown{position:relative;display:flex;align-items:center;gap:calc(9 * var(--lhc-unit,1px));height:calc(50 * var(--lhc-unit,1px));box-sizing:border-box;padding:0 calc(14 * var(--lhc-unit,1px));border-bottom:1px solid #ededed;white-space:nowrap;min-width:0}
.marksix-clock,.marksix-bell{width:calc(28 * var(--lhc-unit,1px));height:calc(28 * var(--lhc-unit,1px));color:#8c9699;flex-shrink:0;stroke-width:1.7}
.marksix-current,.marksix-issue{font-family:Arial,sans-serif;font-size:max(9px,calc(15 * var(--lhc-unit,1px)));font-weight:400;font-variant-numeric:tabular-nums;white-space:nowrap;flex:0 0 auto;line-height:1.2;letter-spacing:-.15px}
.marksix-current small,.marksix-issue small{font-family:inherit;font-size:inherit;font-weight:400;margin-left:calc(2 * var(--lhc-unit,1px))}
.marksix-timer{font-size:calc(20 * var(--lhc-unit,1px));white-space:nowrap;line-height:1.2}
.marksix-timer b{font-family:Arial,sans-serif;font-weight:400;color:#ad6170;font-variant-numeric:tabular-nums}
.marksix-refresh{display:grid;place-items:center;flex:0 0 auto;width:calc(29 * var(--lhc-unit,1px));height:calc(29 * var(--lhc-unit,1px));padding:calc(4 * var(--lhc-unit,1px));border:calc(2 * var(--lhc-unit,1px)) solid #9d83d9;border-radius:50%;color:#9d83d9;margin-left:calc(2 * var(--lhc-unit,1px))}
.marksix-refresh svg{width:100%;height:100%;stroke-width:1.6}
.marksix-refresh .refreshing{animation:marksix-refresh-spin .8s linear infinite}
.marksix-refresh:disabled,.marksix-latest:disabled{opacity:.55;cursor:not-allowed}
.marksix-refresh:focus-visible,.marksix-latest:focus-visible{outline:2px solid #9977dc;outline-offset:-2px}
.marksix-phase{position:absolute;right:calc(13 * var(--lhc-unit,1px));bottom:0;background:#fff;font-size:max(8px,calc(11 * var(--lhc-unit,1px)));line-height:1.1;color:#8d6370;white-space:nowrap}
.marksix-latest{display:flex;align-items:center;gap:calc(10 * var(--lhc-unit,1px));width:100%;height:calc(70 * var(--lhc-unit,1px));box-sizing:border-box;padding:0 calc(14 * var(--lhc-unit,1px));text-align:left;min-width:0;overflow:hidden}
.marksix-issue{font-size:max(9px,calc(14 * var(--lhc-unit,1px)))}
.marksix-chips{--marksix-ball-size:calc(34 * var(--lhc-unit,1px));--marksix-ball-font:calc(18 * var(--lhc-unit,1px));display:flex;align-items:flex-start;gap:calc(3 * var(--lhc-unit,1px));min-width:0;flex-shrink:0}
.marksix-result{display:flex;flex-direction:column;align-items:center;gap:calc(3 * var(--lhc-unit,1px));flex:0 0 auto}
.marksix-result>small{box-sizing:border-box;height:calc(23 * var(--lhc-unit,1px));min-width:calc(25 * var(--lhc-unit,1px));padding:0 calc(3 * var(--lhc-unit,1px));border:1px solid #cbcecb;border-radius:calc(3 * var(--lhc-unit,1px));background:#fff;color:#8a8c87;font-size:calc(20 * var(--lhc-unit,1px));line-height:calc(21 * var(--lhc-unit,1px));font-weight:400;text-align:center;white-space:nowrap}
.marksix-plus{font-weight:700;font-size:calc(18 * var(--lhc-unit,1px));line-height:calc(34 * var(--lhc-unit,1px));margin:0 calc(7 * var(--lhc-unit,1px));color:#141414}
.marksix-expand{margin-left:auto;flex-shrink:0;width:calc(21 * var(--lhc-unit,1px));height:calc(26 * var(--lhc-unit,1px));color:#858b89;transition:transform .15s;stroke-width:1.6}
.marksix-expand.expanded{transform:rotate(90deg)}
.marksix-wait{font-size:max(10px,calc(18 * var(--lhc-unit,1px)));color:#888}
.marksix-legacy-result .marksix-chips{--marksix-ball-size:calc(31 * var(--lhc-unit,1px));--marksix-ball-font:calc(16 * var(--lhc-unit,1px));gap:calc(2 * var(--lhc-unit,1px))}
.marksix-legacy-result .marksix-result>small{font-size:calc(15 * var(--lhc-unit,1px));min-width:calc(22 * var(--lhc-unit,1px));padding:0 calc(2 * var(--lhc-unit,1px))}
.marksix-legacy-result .marksix-plus{margin:0 calc(4 * var(--lhc-unit,1px))}
@keyframes marksix-refresh-spin{to{transform:rotate(360deg)}}
@media(prefers-reduced-motion:reduce){.marksix-refresh .refreshing{animation:none}.marksix-expand{transition:none}}
</style>

<style scoped>
.marksix-current,.marksix-issue{font-size:max(12px,calc(22 * var(--lhc-unit,1px)));letter-spacing:0}
</style>
