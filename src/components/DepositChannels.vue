<script setup lang="ts">
// Preserve the supplied brand artwork without generating or redrawing logos.
// Source: user attachment codex-clipboard-00480b0d-e431-47ff-a285-2aca25908a9f.jpg.
// These are reference-image assets, not independently verified official downloads.
const channels=[
 {name:'OKPay',bounds:[18,98,123,90]},
 {name:'ToPay',bounds:[454,79,145,147]},
 {name:'GOPay',bounds:[909,103,122,123]},
 {name:'KDPay',bounds:[16,421,125,112]},
 {name:'FPay',bounds:[455,404,166,169]},
 {name:'JDPay',bounds:[910,451,151,91]},
 {name:'红豆钱包',bounds:[9,782,131,85]},
 {name:'MPay',bounds:[442,747,139,145]},
 {name:'USDT-TRC20',bounds:[914,766,102,108]},
].map(channel=>{
 const [x,y,width,height]=channel.bounds as [number,number,number,number]
 const scale=42/Math.max(width,height)
 return {...channel,style:{width:`${1280*scale}px`,height:`${960*scale}px`,left:`${(44-width*scale)/2-x*scale}px`,top:`${(44-height*scale)/2-y*scale}px`}}
})
</script>
<template>
 <ul class="deposit-channels" aria-label="充值通道图标" tabindex="0">
  <li v-for="channel in channels" :key="channel.name"><span class="channel-logo" aria-hidden="true"><img src="/payment-channels/brand-reference.jpg" alt="" width="1280" height="960" :style="channel.style" decoding="async"/></span><span>{{channel.name}}</span></li>
 </ul>
</template>
<style scoped>
.deposit-channels{display:flex;flex-wrap:nowrap;justify-content:space-between;gap:12px;list-style:none;padding:0 0 10px;margin:0 0 23px;overflow-x:auto;overscroll-behavior-x:contain;-webkit-overflow-scrolling:touch;scrollbar-width:thin;scrollbar-color:#d9d2e3 transparent}.deposit-channels li{display:flex;flex-direction:column;align-items:center;justify-content:flex-start;flex:0 0 auto;min-width:60px;gap:7px;color:#5e5868;font-size:11px;line-height:1.5;white-space:nowrap}.channel-logo{position:relative;display:block;width:44px;height:44px;overflow:hidden;background:#fff;flex-shrink:0}.channel-logo img{position:absolute;display:block;max-width:none;max-height:none;user-select:none;pointer-events:none}.deposit-channels:focus-visible{outline:2px solid #8764d1;outline-offset:4px}.deposit-channels::-webkit-scrollbar{height:4px}.deposit-channels::-webkit-scrollbar-thumb{background:#d9d2e3;border-radius:4px}@media(max-width:760px){.deposit-channels{justify-content:flex-start;gap:14px;margin-bottom:21px}.deposit-channels li{min-width:62px;font-size:11px}}
</style>
