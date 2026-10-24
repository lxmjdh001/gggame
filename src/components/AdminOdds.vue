<script setup lang="ts">
import {ref,computed,onMounted,watch} from 'vue'
import {Save,RefreshCw,SlidersHorizontal} from 'lucide-vue-next'
import {api,sessionNames,loadSessionNames} from '../api/account'
import {type Market,type MarketBook,type Quote,oddsMilli} from '../api/markets'
import {markSixGroups} from '../lib/marksix'
import {currentZodiac,supportsCurrentZodiacOdds,shortOdds} from '../lib/marksixOdds'
const emit=defineEmits<{notify:[message:string]}>()
const session=ref('ssc-10'),book=ref<MarketBook|null>(null)
const drafts=ref<Record<string,Record<string,string>>>({}),enabled=ref<Record<string,boolean>>({})
const category=ref('all'),group=ref('all'),bulk=ref('1.96'),error=ref(''),busy=ref(false),loading=ref(false),dirty=ref(false)
const tab=ref('zodiac_tail'),position=ref('all'),search=ref(''),presetInfo=ref('')
let revision=0
const isK3=computed(()=>session.value.startsWith('k3-'))
const isMarkSix=computed(()=>!!book.value?.marksix_context)
const groups=computed(()=>[...new Set(book.value?.markets.map(m=>m.group)??[])])
const lhcTabs=computed(()=>[...markSixGroups,{id:'linked_numbers',label:'连码（前台已隐藏）'}].filter(g=>book.value?.markets.some(m=>m.ui_group===g.id)))
const currentAnimal=computed(()=>book.value?.marksix_context?.zodiacs.find(z=>z.value===currentZodiac(book.value?.marksix_context))?.label??'当年生肖')
const visible=computed(()=>book.value?.markets.filter(m=>isMarkSix.value
 ? m.ui_group===tab.value&&(position.value==='all'||String(m.positions[0])===position.value)&&(!search.value||`${m.group}${m.label}`.includes(search.value.trim()))
 : (category.value==='all'||m.category===category.value)&&(group.value==='all'||m.group===group.value))??[])
interface OddsRow {key:string;name:string;markets:Market[]}
const rows=computed(()=>{
 const grouped=new Map<string,OddsRow>()
 for(const m of visible.value){
  const suffix=m.selection_schema?m.id:m.kind==='lhc_color'?m.pick:''
  const key=`${m.group}|${suffix}`
  const name=m.selection_schema?m.label:m.kind==='lhc_color'?`${m.group} · ${m.label}`:m.ui_group==='position_numbers'?m.group.replace(/^正码/,'正特码'):m.group
  if(!grouped.has(key))grouped.set(key,{key,name,markets:[]})
  grouped.get(key)!.markets.push(m)
 }
 return [...grouped.values()]
})
function install(quotes:Quote[]){
 drafts.value=Object.fromEntries(quotes.map(q=>[q.market_id,{base:shortOdds(q.odds_milli),current:q.current_zodiac_odds_milli?shortOdds(q.current_zodiac_odds_milli):'',zero:q.zero_tail_odds_milli?shortOdds(q.zero_tail_odds_milli):'',...Object.fromEntries(Object.entries(q.tier_odds_milli??{}).map(([k,v])=>[`tier_${k}`,shortOdds(v)]))}]))
 enabled.value=Object.fromEntries(quotes.map(q=>[q.market_id,q.enabled]))
}
async function load(){
 const v=++revision;loading.value=true;error.value=''
 try{
  const d=await api<MarketBook>(`/markets/${session.value}`);if(v!==revision)return
  book.value=d;install(d.quotes);dirty.value=false;group.value='all';presetInfo.value=''
 }catch(e){if(v===revision)error.value=(e as Error).message}finally{if(v===revision)loading.value=false}
}
watch(session,()=>{book.value=null;tab.value='zodiac_tail';position.value='all';search.value='';bulk.value=session.value.startsWith('k3-')||session.value.startsWith('lhc-')?'1':'1.96';void load()})
watch(tab,()=>{position.value='all';search.value=''})
onMounted(()=>{void loadSessionNames();void load()})
function setField(id:string,field:string,value:string){drafts.value[id]={...drafts.value[id],[field]:value};dirty.value=true}
function common(row:OddsRow,field:string){const values=[...new Set(row.markets.map(m=>drafts.value[m.id]?.[field]??''))];return values.length===1?values[0]:''}
function mixed(row:OddsRow,field:string){return new Set(row.markets.map(m=>drafts.value[m.id]?.[field]??'')).size>1}
function setRow(row:OddsRow,field:string,value:string){for(const m of row.markets)setField(m.id,field,value)}
function input(event:Event){return (event.target as HTMLInputElement).value}
function apply(){try{oddsMilli(bulk.value);for(const m of visible.value){setField(m.id,'base',bulk.value);for(const tier of m.payout_tiers??[])setField(m.id,`tier_${tier.key}`,bulk.value)};error.value=''}catch(e){error.value=(e as Error).message}}
async function preset(){
 if(busy.value||loading.value||dirty.value)return
 busy.value=true;error.value=''
 try{
  const p=await api<{profile_id:string;quotes:Quote[];configured:number;preserved:number}>(`/admin/marksix-odds-profile/${session.value}`)
  install(p.quotes);dirty.value=true;presetInfo.value=`已套用你指定的倍率：${p.configured}项；未指定的${p.preserved}项和接单开关保留原值。尚未保存。`
 }catch(e){error.value=(e as Error).message}finally{busy.value=false}
}
function quotesForSave():Quote[]{
 return book.value!.markets.map(m=>{
  const d=drafts.value[m.id]??{}
  const parse=(label:string,value:string)=>{try{return oddsMilli(value)}catch(e){throw new Error(`${m.group} · ${m.label} · ${label}：${(e as Error).message}`)}}
  return {market_id:m.id,odds_milli:parse('普通赔率',d.base??''),enabled:enabled.value[m.id]??false,
   ...(d.current?{current_zodiac_odds_milli:parse('本年生肖赔率',d.current)}:{}),...(d.zero?{zero_tail_odds_milli:parse('含0尾赔率',d.zero)}:{}),
   ...(m.payout_tiers?.length?{tier_odds_milli:Object.fromEntries(m.payout_tiers.map(t=>[t.key,parse(t.label,d[`tier_${t.key}`]??'')]))}:{})}
 })
}
async function save(){
 if(!book.value||busy.value||loading.value)return
 busy.value=true;error.value=''
 try{await api(`/admin/markets/${session.value}`,'PUT',{quotes:quotesForSave()});dirty.value=false;presetInfo.value='';emit('notify','该场次赔率已保存，仅新订单生效；旧订单保持下单时的锁定赔率')}
 catch(e){error.value=(e as Error).message}finally{busy.value=false}
}
function example(row:OddsRow){const value=common(row,'base');return value&&Number.isFinite(Number(value))?`${(Number(value)*100).toFixed(2)}元`:'以各选项赔率为准'}
</script>
<template>
 <div class="odds-admin">
  <section class="surface">
   <div class="surface-heading"><div><h3>{{isMarkSix?'六合彩赔率设置':'场次赔率设置'}}</h3><p class="form-hint">赔率都是含本金的返还倍数。例：1.98倍，投注100元中奖返还198元（净赢98元）。最多三位小数。</p></div><span class="badge">仅新订单生效</span></div>
   <div class="odds-filters"><label>选择场次<select v-model="session" :disabled="busy||loading||dirty" aria-label="赔率场次"><option v-for="(name,id) in sessionNames" :key="id" :value="id">{{name}}</option></select></label><template v-if="!isMarkSix"><label>玩法类别<select v-model="category" aria-label="赔率玩法类别"><option value="all">全部玩法</option><option value="two_sides">两面玩法</option><option value="regular">常规玩法</option></select></label><label>玩法分组<select v-model="group" aria-label="赔率玩法分组"><option value="all">全部分组</option><option v-for="g in groups" :key="g">{{g}}</option></select></label></template></div>
   <div v-if="isMarkSix" class="lhc-preset"><div><strong>你指定的六合彩倍率方案</strong><p>号码、两面、色波、生肖和组合分别设置；正特码与正特1–6独立定价。本年生肖为{{currentAnimal}}，随开奖期的农历年份自动切换。</p></div><button class="secondary-button" :disabled="busy||loading||dirty" @click="preset">套用指定倍率</button></div>
   <p v-if="presetInfo" class="lhc-status" role="status">{{presetInfo}}</p><p v-if="dirty" class="form-hint">有未保存修改。保存作用于当前场次的全部玩法；重新读取可放弃修改，保存后可切换场次。</p>
  </section>
  <div v-if="error" class="form-error" role="alert">{{error}}</div><p v-if="loading" class="loading-line">正在读取赔率…</p>
  <section v-if="book&&isMarkSix" class="surface lhc-editor">
   <nav class="lhc-odds-tabs" aria-label="六合彩赔率玩法"><button v-for="t in lhcTabs" :key="t.id" :aria-pressed="tab===t.id" :class="{active:tab===t.id}" @click="tab=t.id">{{t.label}}</button></nav>
   <div class="lhc-filter-row"><label v-if="tab==='position_numbers'||tab==='position_faces'">位置<select v-model="position" aria-label="赔率位置"><option value="all">全部六个位置</option><option v-for="p in 6" :key="p" :value="String(p)">第{{p}}个正码</option></select></label><label>查找选项<input v-model="search" placeholder="例如：红波、合数、2连肖" aria-label="查找赔率选项"/></label><span>{{rows.length}}组 · {{visible.length}}个选项</span></div>
   <p v-if="tab==='position_numbers'" class="lhc-explanation">指定方案：正特码每个位置的号码42倍；两面、合数、尾大小1.98倍；红波2.7倍、蓝波/绿波2.9倍。与“正特1–6”独立定价，实际保存值见输入框。</p>
   <p v-if="tab==='position_faces'" class="lhc-explanation">指定方案：正特1–6（原正码1–6）的两面、合数、尾大小1.96倍；红波2.65倍、蓝波/绿波2.75倍。与“正特码”独立定价，实际保存值见输入框。</p>
   <p v-if="tab==='miss'" class="lhc-explanation">指定方案的12不中为1.9倍，未改成其他数值；当前保存值以下方输入框为准。</p>
   <p v-if="tab==='linked_numbers'" class="lhc-explanation">连码前台入口保持隐藏。二中特按命中档位派彩一次：1正码+1特码为“中特”，2个正码为“中二”，不叠加。</p>
   <div class="lhc-odds-cards"><article v-for="row in rows" :key="row.key" class="lhc-odds-card">
    <header><div><h4>{{row.name}}</h4><p>{{row.markets.map(m=>m.label).join('、')}}</p></div><small>{{row.markets.filter(m=>enabled[m.id]).length}}/{{row.markets.length}}项接单</small></header>
    <div class="lhc-rate-fields">
     <template v-if="row.markets[0]?.payout_tiers?.length"><label v-for="tier in row.markets[0].payout_tiers" :key="tier.key">{{tier.label==='中特'?'中特（1正码＋1特码）':tier.label==='中二'?'中二（2个正码）':tier.label}}<div><input :value="common(row,`tier_${tier.key}`)" :disabled="busy||loading" :aria-label="`${row.name}${tier.label}赔率`" inputmode="decimal" @input="setRow(row,`tier_${tier.key}`,input($event))"/><span>倍</span></div></label></template>
     <label v-else>{{supportsCurrentZodiacOdds(row.markets[0]!)?'普通（不含本年生肖）':row.markets[0]?.kind==='lhc_link_tail'?'普通（不含0尾）':'中奖返还倍数'}}<div><input :value="common(row,'base')" :placeholder="mixed(row,'base')?'各项不同':''" :disabled="busy||loading" :aria-label="`${row.name}普通赔率`" inputmode="decimal" @input="setRow(row,'base',input($event))"/><span>倍</span></div></label>
     <label v-if="supportsCurrentZodiacOdds(row.markets[0]!)" class="conditional-field">{{row.markets[0]?.selection_schema?'组合含':'选中'}}本年生肖（{{currentAnimal}}）<div><input :value="common(row,'current')" :placeholder="mixed(row,'current')?'各项不同':'留空沿用普通'" :disabled="busy||loading" :aria-label="`${row.name}本年生肖赔率`" inputmode="decimal" @input="setRow(row,'current',input($event))"/><span>倍</span></div></label>
     <label v-if="row.markets[0]?.kind==='lhc_link_tail'" class="conditional-field">组合含0尾<div><input :value="common(row,'zero')" :placeholder="mixed(row,'zero')?'各项不同':'留空沿用普通'" :disabled="busy||loading" :aria-label="`${row.name}含0尾赔率`" inputmode="decimal" @input="setRow(row,'zero',input($event))"/><span>倍</span></div></label>
    </div>
    <p class="lhc-example" v-if="!row.markets[0]?.payout_tiers?.length">普通赔率示例：投注100元，中奖返还{{example(row)}}（含本金）<span v-if="(row.markets[0]?.max_payout_multiplier??1)>1">；按出现次数派彩，最多{{row.markets[0]?.max_payout_multiplier}}次</span>。</p>
    <details class="lhc-details"><summary>查看{{row.markets.length>1?'各选项/接单开关':'判奖规则/接单开关'}}<span>上方改价会统一修改本组，下方可单独微调</span></summary><p>{{row.markets[0]?.rule}}</p><div class="lhc-option-grid"><label v-for="m in row.markets" :key="m.id"><span>{{m.label}}</span><input v-if="!m.payout_tiers?.length" :value="drafts[m.id]?.base" :disabled="busy||loading" :aria-label="`${m.group}${m.label}单项赔率`" inputmode="decimal" @input="setField(m.id,'base',input($event))"/><span v-else>分档见上方</span><input v-model="enabled[m.id]" type="checkbox" :disabled="busy||loading" :aria-label="`${m.group}${m.label}接单`" @change="dirty=true"/></label></div></details>
   </article></div><p v-if="!rows.length" class="form-hint">没有匹配的选项。</p>
  </section>
  <section v-else-if="book" class="surface admin-data"><p class="form-hint">{{isK3?'快三出现围骰时，大小、单双、三军不中奖、不退本；三军每注只派彩一次。':'每个位置、选项与场次可独立定价。'}}</p><div class="odds-bulk"><SlidersHorizontal :size="17"/><span>当前{{visible.length}}项统一为</span><input v-model="bulk" aria-label="批量赔率" inputmode="decimal"/><button class="secondary-button" :disabled="busy||loading" @click="apply">应用到当前筛选</button></div><div class="data-table-wrap"><table class="data-table settings-table"><thead><tr><th>位置/玩法</th><th>选项</th><th>赔率（含本金）</th><th>接单</th><th>规则</th></tr></thead><tbody><tr v-for="m in visible" :key="m.id"><td>{{m.group}}</td><td>{{m.label}}</td><td><input :value="drafts[m.id]?.base" :disabled="busy||loading" :aria-label="`${m.group}${m.label}赔率`" inputmode="decimal" @input="setField(m.id,'base',input($event))"/></td><td><input type="checkbox" v-model="enabled[m.id]" :disabled="busy||loading" :aria-label="`${m.group}${m.label}接单`" @change="dirty=true"/></td><td class="odds-rule-cell">{{m.rule}}</td></tr></tbody></table></div></section>
  <div class="settings-save"><span>{{sessionNames[session]}} · {{dirty?'未保存修改':'已读取保存值'}}</span><div class="odds-save-actions"><button class="secondary-button" :disabled="busy||loading" @click="load"><RefreshCw :size="15"/>重新读取</button><button class="primary-button" :disabled="!book||busy||loading||!dirty" @click="save"><Save :size="15"/>{{busy?'处理中…':'保存当前场次全部玩法'}}</button></div></div>
 </div>
</template>
<style scoped>
.lhc-preset{display:flex;align-items:center;justify-content:space-between;gap:18px;margin-top:18px;padding:16px;border:1px solid #ddd6fa;border-radius:10px;background:#f7f5ff}.lhc-preset strong{font-size:14px;color:#6048a3}.lhc-preset p{max-width:680px;margin:7px 0 0;font-size:12px;color:#786b8e;line-height:1.7}.lhc-preset button{flex-shrink:0}.lhc-status{font-size:13px;line-height:1.7;color:#6852aa}.lhc-editor{padding:20px}.lhc-odds-tabs{display:flex;flex-wrap:wrap;gap:8px;padding-bottom:16px;border-bottom:1px solid #ebe7f3}.lhc-odds-tabs button{padding:9px 13px;border:1px solid #e5dfef;border-radius:7px;font-size:13px;color:#78688c;background:#fff}.lhc-odds-tabs button.active{background:#7354cc;border-color:#7354cc;color:white}.lhc-filter-row{display:flex;align-items:end;gap:14px;flex-wrap:wrap;margin:18px 0}.lhc-filter-row label{display:grid;gap:6px;font-size:12px;color:#84758e}.lhc-filter-row select,.lhc-filter-row input{padding:9px 12px;border:1px solid #ddd5e9;border-radius:6px;background:white;min-height:38px;color:#51465d}.lhc-filter-row>span{font-size:12px;color:#91839b;padding-bottom:10px}.lhc-explanation{padding:12px;border-radius:7px;background:#f5f2fc;font-size:12px;line-height:1.8;color:#77608c}.lhc-odds-cards{display:grid;gap:14px;grid-template-columns:repeat(2,minmax(0,1fr))}.lhc-odds-card{min-width:0;border:1px solid #e8e1ef;border-radius:10px;padding:17px;background:#fff}.lhc-odds-card>header{display:flex;gap:10px;align-items:start;justify-content:space-between}.lhc-odds-card h4{margin:0;font-size:15px;color:#51405f}.lhc-odds-card header p{margin:7px 0 0;font-size:11px;line-height:1.6;max-height:38px;overflow:auto;overflow-wrap:anywhere;color:#918297}.lhc-odds-card header small{font-size:11px;white-space:nowrap;color:#96899d}.lhc-rate-fields{display:flex;flex-wrap:wrap;gap:12px;margin-top:17px}.lhc-rate-fields>label{display:grid;gap:7px;flex:1;min-width:140px;color:#756380;font-size:12px}.lhc-rate-fields label>div{display:flex;align-items:center;gap:8px}.lhc-rate-fields input{width:100%;max-width:180px;min-width:0;height:40px;border:1px solid #d9cde9;border-radius:6px;padding:8px 10px;font-size:18px;font-variant-numeric:tabular-nums;color:#66509c;background:#fcfaff}.lhc-rate-fields input:focus{outline:2px solid #bda4e7;outline-offset:1px}.lhc-rate-fields input::placeholder{font-size:11px}.lhc-rate-fields .conditional-field input{background:#fffbf1;border-color:#e8d5aa;color:#957332}.lhc-example{font-size:11px;line-height:1.7;color:#9a8c9e;margin:13px 0}.lhc-details{margin-top:13px;padding-top:11px;border-top:1px solid #eee8f3;font-size:11px;color:#8f7d9e}.lhc-details summary{cursor:pointer;line-height:1.8}.lhc-details summary span{display:block;font-size:10px;color:#aa9bb4;margin-left:12px}.lhc-details>p{line-height:1.8}.lhc-option-grid{display:grid;gap:6px;grid-template-columns:repeat(2,minmax(0,1fr));margin-top:10px}.lhc-option-grid label{display:flex;align-items:center;gap:5px;padding:5px;border:1px solid #eee7f5;border-radius:5px;min-width:0}.lhc-option-grid label>span:first-child{min-width:28px}.lhc-option-grid input:not([type=checkbox]){width:65px;min-width:0;flex:1;border:1px solid #e3d9ed;border-radius:4px;padding:5px;font-size:12px}.lhc-option-grid input[type=checkbox]{accent-color:#7b57c3;width:15px;height:15px;margin-left:auto}.odds-save-actions{display:flex;gap:8px}.settings-save{gap:12px;flex-wrap:wrap}.settings-save>span{font-size:12px}
@media(max-width:1000px){.lhc-odds-cards{grid-template-columns:1fr}}@media(max-width:600px){.lhc-preset{align-items:stretch;flex-direction:column;padding:13px}.lhc-editor{padding:12px}.lhc-odds-tabs{gap:6px}.lhc-odds-tabs button{font-size:11px;padding:8px}.lhc-odds-card{padding:13px}.lhc-filter-row{gap:9px}.lhc-filter-row input{width:180px}.lhc-rate-fields>label{min-width:120px}.lhc-rate-fields input{max-width:100%}.odds-save-actions{width:100%;flex-wrap:wrap}.odds-save-actions button{flex:1;justify-content:center;white-space:normal}.settings-save{padding:12px}.lhc-option-grid{grid-template-columns:1fr}}
</style>
