<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue'
import {api,datetime,user} from '../api/account'
interface Slot{domain:string;enabled:boolean;dns:string;ssl:string;expires?:string;checked?:string;error?:string}
interface Config{version:number;primary:string;ip:string;slots:Slot[];busy:boolean;message:string}
const emit=defineEmits<{notify:[message:string]}>()
const config=ref<Config|null>(null),names=ref<string[]>(Array(5).fill('')),loading=ref(false),sending=ref(false),error=ref('')
const dirty=computed(()=>!!config.value&&names.value.some((name,i)=>name.trim().toLowerCase()!==(config.value!.slots[i]?.domain??'')))
const locked=computed(()=>loading.value||sending.value||!!config.value?.busy)
let alive=true,timer:ReturnType<typeof setTimeout>|undefined,revision=0
const owner=user.value?.id
function valid(){return alive&&owner===user.value?.id&&user.value?.role==='admin'}
function apply(next:Config,replace=false){config.value=next;if(replace)names.value=next.slots.map(slot=>slot.domain);clearTimeout(timer);if(next.busy)timer=setTimeout(()=>void load(false),1800)}
async function load(replace=true){if(!valid()||loading.value||sending.value)return;const seq=++revision;loading.value=true;error.value='';try{const next=await api<Config>('/admin/domains');if(valid()&&seq===revision)apply(next,replace)}catch(e){if(valid())error.value=(e as Error).message}finally{if(valid())loading.value=false}}
async function save(){if(!config.value||locked.value||!valid())return;sending.value=true;error.value='';try{const next=await api<Config>('/admin/domains','PUT',{version:config.value.version,domains:names.value});if(valid()){apply(next,true);emit('notify','备用域名已保存')}}catch(e){if(valid())error.value=(e as Error).message}finally{if(valid())sending.value=false}}
async function action(index:number,action:string){if(!config.value||locked.value||dirty.value||!valid())return;sending.value=true;error.value='';try{const next=await api<Config>('/admin/domains/actions','POST',{version:config.value.version,index,action});if(valid())apply(next)}catch(e){if(valid())error.value=(e as Error).message}finally{if(valid())sending.value=false}}
onMounted(()=>void load());onUnmounted(()=>{alive=false;revision++;clearTimeout(timer)})
</script>
<template>
<section class="domains-panel">
 <header><div><h2>备用网站</h2><p>同一个网站可同时使用多个域名，分别控制访问状态。</p></div><button class="secondary-button" :disabled="loading||sending" @click="load()">{{dirty?'放弃修改并刷新':'刷新状态'}}</button></header>
 <p v-if="error" class="form-error" role="alert">{{error}}</p>
 <p v-if="loading&&!config">正在读取域名配置…</p>
 <template v-if="config">
 <div class="domain-primary surface"><span>当前主域名</span><a :href="`https://${config.primary}`" target="_blank" rel="noopener">{{config.primary}}</a><span class="badge active">已启用</span><small>服务器 IP：{{config.ip}}</small></div>
 <details class="domain-guide surface" open><summary>域名解析方法</summary><ol><li>在购买域名的服务商后台打开“DNS / 域名解析”，添加一条 <b>A 记录</b>，记录值填写 <b>{{config.ip}}</b>。</li><li>使用整个域名（如 example.com），主机记录填 <b>@</b>；使用子域名（如 www.example.com），主机记录填 <b>www</b>。每个完整域名占一个位置，系统不会自动添加 www。</li><li>TTL 使用默认值。删除指向其他服务器的 A / AAAA 记录；使用 CDN 时先关闭代理，设为“仅 DNS”，再等待解析生效。</li><li>在下面填写域名并保存，点击“检测解析”，通过后点击“申请 SSL 并启用”。系统自动处理证书，任务完成后即可用 HTTPS 访问。</li></ol><p>只填域名，不带 https://、端口或路径。启用的域名使用同一套会员与数据，各域名需要分别登录。更换或清空位置会禁用原域名，主域名不受影响。</p><p>禁用域名返回 404（证书续期验证路径除外）。新域名尚未签发证书时，HTTPS 无法建立可信连接；签发后再禁用，HTTP / HTTPS 均返回 404。</p></details>
 <form class="domain-form surface" @submit.prevent="save">
 <div class="domain-form-heading"><h3>备用域名（最多 5 个）</h3><button class="primary-button" :disabled="locked||!dirty">{{sending?'保存中…':'保存域名'}}</button></div>
 <article v-for="(slot,i) in config.slots" :key="i" class="domain-row">
  <label :for="`backup-domain-${i}`">备用域名 {{i+1}}</label><input :id="`backup-domain-${i}`" v-model="names[i]" :disabled="locked" autocapitalize="none" autocomplete="off" spellcheck="false" maxlength="253" placeholder="例如 backup.example.com"/>
  <template v-if="slot.domain"><div class="domain-state"><span class="badge" :class="slot.enabled?'active':'disabled'">{{slot.enabled?'已启用':'已禁用'}}</span><span>解析：{{slot.dns==='unchecked'?'未检测':slot.dns==='failed'?'未通过':slot.dns}}</span><span>SSL：{{slot.ssl==='ready'?'已签发':slot.ssl==='expired'?'需更新':'未签发'}}</span><span v-if="slot.expires">有效至 {{datetime(slot.expires)}}</span></div><div class="domain-actions"><button type="button" class="secondary-button" :disabled="locked||dirty" @click="action(i,'check')">检测解析</button><button type="button" class="secondary-button" :disabled="locked||dirty" @click="action(i,'enable')">{{slot.enabled?'检测 / 更新 SSL':'申请 SSL 并启用'}}</button><button type="button" class="secondary-button" :disabled="locked||dirty||!slot.enabled" @click="action(i,'disable')">禁用</button><a v-if="slot.enabled" :href="`https://${slot.domain}`" target="_blank" rel="noopener">访问网站 ↗</a></div><p v-if="slot.error" class="form-error" role="alert">{{slot.error}}</p></template>
 </article>
 <p v-if="dirty" class="domain-muted">输入内容已修改，请先保存，再执行检测、启用或禁用。</p>
 </form>
 <p class="domain-muted" role="status">{{config.busy?'正在处理域名任务，可能需要数分钟，状态会自动刷新。':config.message}}</p>
 </template>
</section>
</template>
<style scoped>
.domains-panel{display:grid;gap:18px;color:#62556f}.domains-panel>header,.domain-form-heading{display:flex;justify-content:space-between;align-items:center;gap:12px}.domains-panel h2,.domains-panel h3{margin:0}.domains-panel header p,.domain-muted{font-size:13px;color:#98899f;line-height:1.8}.domain-primary{display:flex;align-items:center;gap:14px;flex-wrap:wrap;padding:18px}.domain-primary a{font-size:18px;color:#875bcc}.domain-primary small{margin-left:auto;color:#998ca1}.domain-guide{padding:18px;font-size:13px;line-height:1.9}.domain-guide summary{cursor:pointer;font-weight:600}.domain-guide ol{padding-left:23px;margin:12px 0}.domain-guide p{color:#94839f;margin:10px 0 0}.domain-form{padding:20px}.domain-row{padding:19px 0;border-bottom:1px solid #eee7f3}.domain-row:last-of-type{border-bottom:0}.domain-row>label{display:block;font-size:13px;margin-bottom:9px}.domain-row>input{width:100%;max-width:580px;padding:12px;border:1px solid #ded4e9;border-radius:6px;background:white;color:#665373;font:inherit}.domain-row>input:focus{outline:2px solid #c3a5ec}.domain-state{display:flex;flex-wrap:wrap;gap:13px;align-items:center;font-size:12px;margin:12px 0;color:#9a89a7}.domain-actions{display:flex;gap:9px;flex-wrap:wrap;align-items:center}.domain-actions a{font-size:12px;color:#9564c5}.domains-panel button:disabled{opacity:.5;cursor:not-allowed}@media(max-width:600px){.domains-panel>header{align-items:flex-start}.domain-form{padding:15px}.domain-primary small{margin-left:0;width:100%}.domain-actions>button{padding:10px;font-size:12px}.domain-guide{padding:15px}.domain-primary a{font-size:16px}}
</style>
