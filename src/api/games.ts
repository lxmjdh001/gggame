import { withRequestTimeout } from '../lib/requestTimeout'

export interface Session { id: string; game_id: string; game_name: string; name: string; interval_minutes: number; offset_minutes: number; group: string; positions: number[]; format: 'digits' | 'triple' | 'padded' | 'marksix' | 'sum'; rule: string }
export interface Round { session_id: string; issue: string; source_issue: string; source_group: string; positions: number[]; numbers: number[]; display_numbers: string[]; sum?: number; draw_at: string; published_at: string; origin: 'simulated_history' | 'scheduled' }
export interface State { session: Session; latest: Round | null; server_time: string; expected_issue: string; pending: boolean; next_draw_at: string; bet_close_at: string; betting_open: boolean }
export interface History { items: Round[]; next_before?: string; total: number }
export async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
 return withRequestTimeout(async requestSignal => {
  const response = await fetch(path, { signal: requestSignal, cache: 'no-store' }).catch(() => {
    const error = new Error('网络连接异常，请检查网络后重试')
    if (requestSignal.aborted) error.name = 'AbortError'
    throw error
  })
  if (!response.ok) throw new Error(`服务暂不可用（${response.status}）`)
  const body = await response.json().catch((e: unknown) => { if (requestSignal.aborted) throw e; throw new Error('服务暂不可用，请稍后重试') })
  if (!body || typeof body !== 'object' || !('data' in body)) throw new Error('服务暂不可用，请稍后重试')
  return body.data as T
 }, 8000, signal)
}
export interface PickSubmission { unitCents: number; requestKey: string; numbers: number[]; amountCents: number; sessionID: string; sessionName: string; targetIssue: string }
