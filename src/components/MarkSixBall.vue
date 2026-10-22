<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ number: number; color?: 'red' | 'blue' | 'green' }>()
const red = new Set([1, 2, 7, 8, 12, 13, 18, 19, 23, 24, 29, 30, 34, 35, 40, 45, 46])
const blue = new Set([3, 4, 9, 10, 14, 15, 20, 25, 26, 31, 36, 37, 41, 42, 47, 48])
const ballColor = computed(() => props.color ?? (red.has(props.number) ? 'red' : blue.has(props.number) ? 'blue' : 'green'))
</script>

<template>
 <span class="marksix-ball" :class="ballColor"><svg class="marksix-ball-spots" viewBox="0 0 100 100" aria-hidden="true"><circle cx="50" cy="8" r="4.5"/><circle cx="86.37" cy="29" r="4.5"/><circle cx="86.37" cy="71" r="4.5"/><circle cx="50" cy="92" r="4.5"/><circle cx="13.63" cy="71" r="4.5"/><circle cx="13.63" cy="29" r="4.5"/></svg><b>{{String(number).padStart(2,'0')}}</b></span>
</template>

<style scoped>
.marksix-ball {
 --marksix-ball-ink: #387a29;
 position: relative;
 display: inline-grid;
 place-items: center;
 flex: 0 0 auto;
 width: var(--marksix-ball-size, 34px);
 height: var(--marksix-ball-size, 34px);
 box-sizing: border-box;
 border-radius: 50%;
 background: var(--marksix-ball-ink);
 box-shadow: inset 0 0 0 1px #17331b35, 0 1px 1px #0003;
 vertical-align: middle;
 isolation: isolate;
}
.marksix-ball.red { --marksix-ball-ink: #b9141c; }
.marksix-ball.blue { --marksix-ball-ink: #174e9c; }
.marksix-ball-spots { position:absolute; inset:0; width:100%; height:100%; fill:#fff; pointer-events:none; }
.marksix-ball b {
 position: relative;
 display: grid;
 place-items: center;
 width: 73%;
 height: 73%;
 box-sizing: border-box;
 border: 1px solid #ffffffb3;
 border-radius: 50%;
 background: radial-gradient(circle at 38% 30%, #fff 0 48%, #eeeee1 82%, #d8dbce 100%);
 box-shadow: 0 0 0 1px var(--marksix-ball-ink), inset 0 0 2px #233d202e;
 color: #171917;
 font-family: Arial, sans-serif;
 font-size: var(--marksix-ball-font, 18px);
 font-weight: 800;
 font-style: normal;
 font-variant-numeric: tabular-nums;
 line-height: 1;
 letter-spacing: -.6px;
 text-shadow: 0 1px #fff;
}
</style>
