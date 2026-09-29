export const API = (() => {
  const base = ''
  // 若服务端启用了 QIANSI_TOKEN，需要在前端保存同一个令牌
  function authHeaders(): Record<string, string> {
    const t = localStorage.getItem('q_token') || ''
    return t ? { Authorization: 'Bearer ' + t } : {}
  }
  async function req<T>(path: string, init: RequestInit = {}): Promise<T> {
    const res = await fetch(base + path, { ...init, headers: { ...authHeaders(), ...(init.headers || {}) } })
    if (!res.ok) {
      let msg = res.statusText
      try { msg = (await res.json()).error || msg } catch {}
      throw new Error(msg)
    }
    if (res.status === 204) return undefined as T
    return res.json()
  }
  const headers = () => ({ 'Content-Type': 'application/json' })
  return {
    get: <T>(p: string) => req<T>(p),
    post: <T>(p: string, body: any) => req<T>(p, { method: 'POST', headers: headers(), body: JSON.stringify(body) }),
    put: <T>(p: string, body: any) => req<T>(p, { method: 'PUT', headers: headers(), body: JSON.stringify(body) }),
    delete: <T>(p: string, body?: any) => req<T>(p, { method: 'DELETE', ...(body ? { headers: headers(), body: JSON.stringify(body) } : {}) }),
  }
})()

export type Person = {
  id: string; name: string; family_name?: string; given_name?: string;
  nickname?: string; gender?: string;
  birthday?: string; birthday_is_lunar?: boolean; avatar_attachment_id?: string;
  phone?: string; wechat?: string; location?: string; notes?: string;
  grade: number; category_id?: number; archived?: boolean;
  created_at: string; updated_at: string;
  category_name?: string; intimacy?: number
}
export type Event = { id: string; title: string; type_id?: number; type_name?: string; type_color?: string; event_date: string; location?: string; locations?: string[]; has_gift?: boolean; gift?: string; summary?: string; created_at: string; updated_at: string; participants?: Person[]; expense_fen?: number; expense_person_id?: string; expenses?: Transaction[] }
export type Memo = { id: string; person_id?: string; speaker: string; content: string; said_at: string; is_promise: boolean; due_date?: string; status: string; created_at: string }
export type Transaction = { id: string; person_id: string; kind: string; direction: string; amount_fen: number; title?: string; occurred_at: string; due_date?: string; settled: boolean; settled_at?: string; created_at: string; person_name?: string; repaid_fen?: number; event_id?: string; event_title?: string }
export type Anniversary = { id: string; person_id?: string; title: string; date: string; is_lunar: boolean; repeat_yearly: boolean; remind_days: string; created_at: string; person_name?: string; next_date?: string; days_until?: number | null }
export type Reminder = { id: string; person_id?: string; ref_type: string; ref_id?: string; title: string; due_at: string; status: string; created_at: string; completed_at?: string; person_name?: string }
export type Category = { id: number; name: string; color: string; icon: string; sort_order: number }
export type Tag = { id: number; name: string; color: string }
export type EventType = { id: number; name: string; color: string; icon: string; is_default: boolean; sort_order: number }
export type Dashboard = { total_people: number; total_events: number; upcoming_days7: number; due_today: number; lend_fen: number; borrow_fen: number; pending_promises: number }
export type GradeDist = { grade: number; count: number }
export type Suggestion = { type: string; person_id?: string; person_name?: string; message: string }
export type Relationship = { id: string; from_person_id: string; to_person_id: string; type: string; remark?: string; created_at: string; from_name?: string; to_name?: string }
export type TimelineItem = { date: string; type: string; title: string; person_id?: string; person_name?: string; id: string }
export type Repayment = { id: string; transaction_id: string; amount_fen: number; occurred_at: string; note?: string }
export type PersonField = { id: string; person_id: string; label: string; value?: string; sort_order?: number }
export type BackupItem = { name: string; size: number; time: string }
export type TrendPoint = { day: string; score: number }
export type SearchResult = { type: string; id: string; title: string; subtitle?: string; date?: string; path: string }

// ===== 枚举本地化：后端存英文，界面统一显示中文 =====
export const KIND_LABEL: Record<string, string> = { loan: '借还', gift: '礼物', expense: '花销', other: '其它' }
export const DIRECTION_LABEL: Record<string, string> = { out: '我支出', in: '我收入' }
export const MEMO_STATUS_LABEL: Record<string, string> = { open: '进行中', fulfilled: '已兑现', broken: '未兑现' }
export const REF_TYPE_LABEL: Record<string, string> = { custom: '手动', anniversary: '纪念日', birthday: '生日', memo: '对话', transaction: '金钱' }
export const TIMELINE_LABEL: Record<string, string> = { event: '往来', memo: '对话', transaction: '金钱', anniversary: '纪念日' }
export const SEARCH_LABEL: Record<string, string> = { person: '人物', event: '往来', memo: '对话', transaction: '金钱', anniversary: '纪念日' }
export const RELATION_TYPES = ['家人', '亲戚', '朋友', '同学', '同事', '邻居', '合作伙伴', '其它']

export const yuan = (fen: number) => (fen / 100).toFixed(2)

/** 元字符串转分；非法输入返回 0，避免把 NaN 写进库 */
export function toFen(input: string | number): number {
  const n = typeof input === 'number' ? input : parseFloat(String(input || '').trim())
  if (!isFinite(n) || n <= 0) return 0
  return Math.round(n * 100)
}

/** 本地日期 YYYY-MM-DD（不用 toISOString，那是 UTC，会差一天） */
export function todayLocal(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
