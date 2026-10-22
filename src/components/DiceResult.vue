<script setup lang="ts">
import { computed } from 'vue'
import DiceFace from './DiceFace.vue'

const props = defineProps<{ numbers: number[] }>()
const valid = computed(() => props.numbers.length === 3 && props.numbers.every(number => Number.isInteger(number) && number >= 1 && number <= 6))
const sum = computed(() => props.numbers.reduce((total, number) => total + number, 0))
const triple = computed(() => valid.value && props.numbers.every(number => number === props.numbers[0]))
const size = computed(() => triple.value ? '豹' : sum.value <= 10 ? '小' : '大')
const parity = computed(() => triple.value ? '豹' : sum.value % 2 ? '单' : '双')
const description = computed(() => `开奖号码 ${props.numbers.join('、')}，${triple.value ? '豹子，大小单双不中奖' : `${size.value}、${parity.value}`}，总点数 ${sum.value}`)
</script>

<template>
 <span v-if="valid" class="dice-result" :class="{'is-triple':triple}" role="img" :aria-label="description">
  <DiceFace v-for="(number,index) in numbers" :key="index" :number="number"/>
  <span class="dice-equals" aria-hidden="true">=</span>
  <span class="dice-result-tag dice-size" :title="triple ? '豹子，大小均不中奖' : undefined" aria-hidden="true">{{size}}</span>
  <span class="dice-result-tag dice-parity" :title="triple ? '豹子，单双均不中奖' : undefined" aria-hidden="true">{{parity}}</span>
  <strong class="dice-sum" aria-hidden="true">{{sum}}</strong>
 </span>
</template>

<style scoped>
.dice-result{--dice-face-size:34px;display:inline-flex;align-items:center;gap:8px;min-width:0;white-space:nowrap;font-family:Arial,'Microsoft YaHei','PingFang SC',sans-serif;font-variant-numeric:tabular-nums}
.dice-equals{font-size:20px;color:#333;font-weight:400}
.dice-result-tag{display:grid;place-items:center;min-width:26px;font-size:20px;color:#26272a}.dice-sum{min-width:26px;text-align:center;font-size:22px;font-weight:600;color:#b83f4d}
@media(max-width:760px){.dice-result{--dice-face-size:30px;gap:6px}.dice-equals{font-size:18px}.dice-result-tag{min-width:21px;font-size:18px}.dice-sum{min-width:23px;font-size:20px}}
@media(max-width:360px){.dice-result{--dice-face-size:25px;gap:4px}.dice-equals{font-size:16px}.dice-result-tag{min-width:18px;font-size:16px}.dice-sum{font-size:18px;min-width:21px}}
</style>
