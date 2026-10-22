import type {Session} from '../api/games'

export function sessionPeriod(session?: Pick<Session,'interval_minutes'>): string {
 return session ? `每${session.interval_minutes}分钟一期` : '定时开奖'
}

export function closeAhead(session: Pick<Session,'id'|'game_id'>, seconds?: number): string {
 const lead = seconds ?? (session.game_id === 'lhc' && session.id !== 'lhc-hk' ? 60 : 10)
 return lead % 60 === 0 ? `${lead / 60}分钟` : `${lead}秒`
}

export function sessionSchedule(session: Session): string {
 return `${session.game_id === 'lhc' ? '自2026年10月15日12:00起，' : ''}${sessionPeriod(session)}，开奖分钟的第00秒开奖，提前${closeAhead(session)}封盘（北京时间）。`
}
