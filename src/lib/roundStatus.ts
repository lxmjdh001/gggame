import type {State} from '../api/games'

export type RoundPhase='syncing'|'paused'|'open'|'sealed'|'drawing'
export const roundStatusLabels:Record<RoundPhase,string>={
 syncing:'同步中，暂不可投注',
 paused:'本场暂停投注',
 open:'投注中，祝您好运',
 sealed:'已封盘，等待开奖',
 drawing:'开奖中，请稍候',
}

export function roundPhase(
 state:Pick<State,'next_draw_at'|'bet_close_at'|'betting_open'>|null,
 now:number,
 enabled:boolean|undefined,
 syncing=false,
):RoundPhase {
 if(syncing||!state||enabled===undefined)return 'syncing'
 if(!enabled)return 'paused'
 const drawAt=Date.parse(state.next_draw_at),closeAt=Date.parse(state.bet_close_at)
 if(!Number.isFinite(now)||!Number.isFinite(drawAt)||!Number.isFinite(closeAt))return 'syncing'
 // Match the displayed :00 draw time; the original timestamp still drives polling
 // and the countdown. State.pending refers to the previous result, not this period.
 const displayedDrawAt=Math.floor(drawAt/60000)*60000
 if(now>=displayedDrawAt)return 'drawing'
 if(now>=closeAt||!state.betting_open)return 'sealed'
 return 'open'
}
