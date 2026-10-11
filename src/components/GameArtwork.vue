<script setup lang="ts">
import type { Game } from '../data'
defineProps<{ game: Game }>()
</script>
<template>
  <div class="game-art" :class="{ dice: game.id === 'k3' }" aria-hidden="true">
    <div class="art-orbit"></div><div class="ticket-art"><span>GOOD LUCK</span><div>✦ ✦ ✦</div><i></i><i></i></div>
    <div v-for="(ball, i) in game.balls" :key="i" class="lottery-ball" :class="`ball-${i}`"><span>{{ ball }}</span></div>
    <i class="spark spark-one">✦</i><i class="spark spark-two">✧</i><i class="art-dot"></i>
  </div>
</template>
