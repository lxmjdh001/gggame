<script setup lang="ts">
import { computed } from 'vue'
import { markSixLunarYear, markSixZodiacLabel } from '../lib/marksixZodiac'
import type { Round, Session } from '../api/games'
import DiceResult from './DiceResult.vue'
import MarkSixBall from './MarkSixBall.vue'
import RacingResult from './RacingResult.vue'
const props = defineProps<{ round: Round; format: Session['format']; compact?: boolean }>()
const lunarYear = computed(() => props.format === 'marksix' ? markSixLunarYear(props.round.draw_at) : null)
const legacyAustralia = computed(() => props.round.session_id === 'lhc-au' && props.round.numbers.length === 8)
function label(number: number, index: number) {
 return legacyAustralia.value ? index < 6 ? `正${index + 1}` : `附${index - 5}` : markSixZodiacLabel(number, lunarYear.value)
}
</script>
<template>
  <div class="result-numbers" :class="[format === 'padded' ? 'racing' : format, { compact }]">
    <DiceResult v-if="format === 'triple'" :numbers="round.numbers"/>
    <RacingResult v-else-if="format === 'padded'" :numbers="round.numbers"/>
    <template v-else-if="format === 'marksix'">
      <template v-for="(number, i) in round.numbers" :key="i">
        <span v-if="i === 6" class="math-sign special-plus" aria-hidden="true">+</span>
        <span class="marksix-number-column" :aria-label="`${i < 6 ? '正码' + (i + 1) : legacyAustralia ? '附加号' + (i - 5) : '特码'} ${number} ${label(number, i)}`">
          <MarkSixBall :number="number"/>
          <small class="marksix-number-zodiac">{{label(number, i)}}</small>
        </span>
      </template>
    </template>
    <template v-else>
      <template v-for="(number, i) in round.display_numbers" :key="i">
        <span v-if="format === 'sum' && i > 0" class="math-sign">+</span>
        <span class="result-number" >{{ number }}</span>
      </template>
      <template v-if="format === 'sum'"><span class="math-sign">=</span><span class="sum-number">{{ round.sum }}</span></template>
    </template>
  </div>
</template>

<style scoped>
.result-numbers.racing{display:block;width:100%;min-width:0}
.result-numbers.marksix{--marksix-ball-size:34px;--marksix-ball-font:18px;align-items:flex-start}
.marksix-number-column{display:flex;flex-direction:column;align-items:center;gap:3px;flex:0 0 auto}
.marksix-number-zodiac{box-sizing:border-box;min-width:25px;height:23px;padding:0 3px;border:1px solid #cbcecb;border-radius:3px;background:#fff;color:#8a8c87;font-size:20px;line-height:21px;font-weight:400;text-align:center;white-space:nowrap}
.result-numbers.marksix .special-plus{display:grid;place-items:center;align-self:flex-start;height:var(--marksix-ball-size);padding:0;color:#171917;font-size:16px}
@media(min-width:761px) and (max-width:1200px){.result-numbers.marksix{--marksix-ball-size:24px;--marksix-ball-font:13px}.marksix-number-zodiac{min-width:20px;height:19px;font-size:16px;line-height:17px}}
@media(max-width:760px){.result-numbers.marksix{--marksix-ball-size:30px;--marksix-ball-font:16px}.marksix-number-zodiac{min-width:23px;height:21px;font-size:18px;line-height:19px}}
@media(max-width:365px){.result-numbers.marksix{--marksix-ball-size:27px;--marksix-ball-font:14px}.marksix-number-zodiac{min-width:21px;height:20px;font-size:16px;line-height:18px}}
</style>
