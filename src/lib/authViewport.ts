// Older Android WebViews do not understand dvh; newer Android keyboards may
// resize only visualViewport. Keep this modal bounded without intercepting touch.
export function observeAuthViewport(update:(height:number)=>void,host:Window=window):()=>void {
 const viewport=host.visualViewport
 let last=0,active=true
 const sync=()=>{
  if(!active||(viewport&&viewport.scale>1.01))return // Do not fight pinch zoom.
  const visible=viewport?.height
  const height=typeof visible==='number'&&Number.isFinite(visible)&&visible>0?visible:host.innerHeight
  if(!Number.isFinite(height)||height<=0)return
  const next=Math.floor(height)
  if(next>0&&next!==last){last=next;update(next)}
 }
 host.addEventListener('resize',sync)
 host.addEventListener('orientationchange',sync)
 viewport?.addEventListener('resize',sync)
 sync()
 return ()=>{
  active=false
  host.removeEventListener('resize',sync)
  host.removeEventListener('orientationchange',sync)
  viewport?.removeEventListener('resize',sync)
 }
}
