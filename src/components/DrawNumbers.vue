<script setup lang="ts">
import type { Round, Session } from '../api/games'
defineProps<{ round: Round; format: Session['format']; compact?: boolean }>()
</script>
<template>
  <div class="result-numbers" :class="[format, { compact }]">
    <template v-for="(number, i) in round.display_numbers" :key="i">
      <span v-if="format === 'sum' && i > 0" class="math-sign">+</span>
      <span v-if="format === 'marksix' && i === 6" class="math-sign special-plus">+</span>
      <span class="result-number" :class="[{ special: format === 'marksix' && i === 6 }, format === 'padded' ? `racer-${round.numbers[i]}` : '']">{{ number }}<small v-if="format === 'marksix' && i === 6">特别号</small></span>
    </template>
    <template v-if="format === 'sum'"><span class="math-sign">=</span><span class="sum-number">{{ round.sum }}</span></template>
    <span v-if="format === 'triple'" class="triple-sum">和值 <b>{{ round.sum }}</b></span>
  </div>
</template>
