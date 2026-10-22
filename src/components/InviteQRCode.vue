<script setup lang="ts">
import {onUnmounted,ref,watch} from 'vue'
const props=defineProps<{url:string}>()
const image=ref(''),failed=ref(false)
let sequence=0
watch(()=>props.url,async url=>{
 const request=++sequence
 image.value='';failed.value=false
 if(!url)return
 try{
  const QRCode=(await import('qrcode')).default
  const result=await QRCode.toDataURL(url,{width:384,margin:4,errorCorrectionLevel:'M',color:{dark:'#35264FFF',light:'#FFFFFFFF'}})
  if(request===sequence)image.value=result
 }catch{if(request===sequence)failed.value=true}
},{immediate:true})
onUnmounted(()=>sequence++)
</script>
<template>
 <figure class="invite-qr">
  <img v-if="image" :src="image" width="176" height="176" alt="邀请注册二维码"/>
  <div v-else class="invite-qr-placeholder" role="status">{{failed?'二维码生成失败，请使用邀请链接':'正在生成二维码…'}}</div>
  <figcaption><strong>扫码加入</strong><span>自动填写我的邀请码</span></figcaption>
 </figure>
</template>
<style scoped>
.invite-qr{flex:0 0 auto;align-self:center;display:flex;flex-direction:column;align-items:center;width:200px;margin:0 16px;padding:12px 12px 16px;border:1px solid #e8dff5;border-radius:16px;background:#fff;box-shadow:0 6px 20px #66508c09;text-align:center}
.invite-qr img{display:block;width:176px;height:176px;max-width:100%;object-fit:contain}
.invite-qr-placeholder{width:176px;height:176px;display:grid;place-items:center;padding:16px;font-size:12px;line-height:1.7;color:#a18bb5}
.invite-qr figcaption{display:flex;flex-direction:column;gap:5px;margin-top:3px}.invite-qr strong{font-size:13px;color:#725693;font-weight:600}.invite-qr figcaption span{font-size:11px;color:#a08aaf}
@media(max-width:640px){.invite-qr{width:184px;margin:0;padding:10px 10px 14px;border-radius:13px}.invite-qr img,.invite-qr-placeholder{width:162px;height:162px}}
</style>
