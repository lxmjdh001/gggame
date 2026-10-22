<script setup lang="ts">
import { useId } from 'vue'
defineProps<{ number: number }>()
const id = useId()
const pips: Record<number, [number, number][]> = {
 1: [[12, 11.5]], 2: [[6.5, 6], [17.5, 17]], 3: [[6.5, 6], [12, 11.5], [17.5, 17]],
 4: [[6.5, 6], [17.5, 6], [6.5, 17], [17.5, 17]],
 5: [[6.5, 6], [17.5, 6], [12, 11.5], [6.5, 17], [17.5, 17]],
 6: [[6.5, 5.5], [17.5, 5.5], [6.5, 11.5], [17.5, 11.5], [6.5, 17.5], [17.5, 17.5]],
}
</script>
<template>
 <svg class="dice-face" :class="{red:number===1||number===4}" viewBox="0 0 24 25" aria-hidden="true" focusable="false">
  <defs><linearGradient :id="`${id}-surface`" x1="0" y1="0" x2=".8" y2="1"><stop stop-color="#fff"/><stop offset=".5" stop-color="#fcfcfa"/><stop offset="1" stop-color="#deded9"/></linearGradient><linearGradient :id="`${id}-pip`" x1="0" y1="0" x2="0" y2="1"><stop :stop-color="number===1||number===4?'#a3242c':'#161719'"/><stop offset="1" :stop-color="number===1||number===4?'#e95059':'#55575a'"/></linearGradient></defs>
  <rect x="1.2" y="3" width="21.6" height="21" rx="3.2" fill="#b8b9b4" stroke="#a7a9a4" stroke-width=".6"/>
  <rect x=".8" y=".8" width="22.4" height="21.7" rx="3.2" :fill="`url(#${id}-surface)`" stroke="#bfc1bb" stroke-width=".8"/>
  <path d="M3 7V4.8A2 2 0 0 1 5 2.8H19" fill="none" stroke="#fff" stroke-width="1.1" stroke-linecap="round"/>
  <circle v-for="(pip,index) in pips[number]" :key="index" :cx="pip[0]" :cy="pip[1]" :r="number===1?3:2" :fill="`url(#${id}-pip)`"/>
 </svg>
</template>
<style scoped>
.dice-face{display:block;width:var(--dice-face-size,30px);height:var(--dice-face-size,30px);flex:0 0 auto;overflow:visible;filter:drop-shadow(0 2px 1px #24282b30)}
</style>
