<script setup lang="ts">
import {onUnmounted,ref} from 'vue'
const props=defineProps<{modelValue:string;label:string;disabled:boolean}>()
const emit=defineEmits<{'update:modelValue':[value:string];busy:[busy:boolean]}>()
const busy=ref(false),error=ref('')
let generation=0,reader:FileReader|null=null
function select(event:Event){
 const input=event.target as HTMLInputElement,file=input.files?.[0];input.value=''
 if(!file||props.disabled||busy.value)return
 error.value=''
 if(!['image/png','image/jpeg'].includes(file.type)||file.size>256*1024){error.value='请选择256KB以内的PNG或JPEG图片';return}
 const ticket=++generation;reader=new FileReader();busy.value=true;emit('busy',true)
 const finish=()=>{if(ticket===generation){busy.value=false;emit('busy',false)}}
 reader.onload=()=>{if(ticket===generation){emit('update:modelValue',String(reader?.result??''));finish()}}
 reader.onerror=()=>{if(ticket===generation){error.value='图片读取失败，请重新选择';finish()}}
 reader.onabort=finish
 reader.readAsDataURL(file)
}
function remove(){if(!props.disabled&&!busy.value){generation++;emit('update:modelValue','');error.value=''}}
onUnmounted(()=>{generation++;reader?.abort();emit('busy',false)})
</script>
<template>
 <section class="qr-editor" :aria-label="label">
  <h4>{{label}}</h4><div v-if="modelValue" class="qr-preview"><img :src="modelValue" :alt="label+'预览'"/><button type="button" :disabled="disabled||busy" @click="remove">移除二维码</button></div><p v-else class="qr-empty">未配置二维码，前台不显示此图片。</p>
  <label class="qr-upload">{{busy?'正在读取图片…':modelValue?'更换二维码图片':'选择二维码图片'}}<input type="file" accept="image/png,image/jpeg" :disabled="disabled||busy" :aria-label="label+'图片'" @change="select"/></label>
  <p v-if="error" role="alert" class="form-error">{{error}}</p><p class="form-hint">支持256KB以内的PNG/JPEG，64–2048像素。请确认二维码对应正确的收款账户、地址及网络；点击下方「保存收款设置」后生效。</p>
 </section>
</template>
<style scoped>
.qr-editor{padding:18px;border:1px solid #e7def1;border-radius:9px;margin:18px 0;background:#fcfaff}.qr-editor h4{font-size:14px;margin:0 0 12px;color:#6a5684}.qr-preview{display:flex;align-items:center;gap:18px;flex-wrap:wrap}.qr-preview img{width:176px;height:176px;max-width:100%;object-fit:contain;background:#fff;border:1px solid #eee;padding:8px}.qr-preview button{color:#a86479;text-decoration:underline;font-size:12px}.qr-empty{font-size:12px;color:#a397ae}.qr-upload{display:flex;flex-direction:column;gap:8px;font-size:12px;color:#745b92;margin:16px 0}.qr-upload input{max-width:100%;font-size:12px}.qr-editor .form-hint{font-size:11px;line-height:1.8;color:#9b8aa9}button:disabled{opacity:.5}
</style>
