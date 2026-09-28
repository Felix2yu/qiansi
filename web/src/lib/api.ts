export const API = (() => {
  const base = ''
  async function req<T>(path: string, init: RequestInit = {}): Promise<T> {
    const res = await fetch(base + path, init)
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
    delete: <T>(p: string) => req<T>(p, { method: 'DELETE' }),
  }
})()

export type Person = {
  id: string; name: string; nickname?: string; gender?: string;
  birthday?: string; birthday_is_lunar?: boolean; avatar_attachment_id?: string;
  phone?: string; wechat?: string; location?: string; notes?: string;
  grade: number; category_id?: number; archived?: boolean;
  created_at: string; updated_at: string;
  category_name?: string; intimacy?: number
}
export type Event = { id: string; title: string; type_id?: number; type_name?: string; type_color?: string; event_date: string; location?: string; summary?: string; created_at: string; updated_at: string; participants?: Person[] }
export type Memo = { id: string; person_id?: string; speaker: string; content: string; said_at: string; is_promise: boolean; due_date?: string; status: string; created_at: string }
export type Transaction = { id: string; person_id: string; kind: string; direction: string; amount_fen: number; title?: string; occurred_at: string; due_date?: string; settled: boolean; settled_at?: string; created_at: string; person_name?: string; repaid_fen?: number }
export type Anniversary = { id: string; person_id?: string; title: string; date: string; is_lunar: boolean; repeat_yearly: boolean; remind_days: string; created_at: string; person_name?: string }
export type Reminder = { id: string; person_id?: string; ref_type: string; ref_id?: string; title: string; due_at: string; status: string; created_at: string; completed_at?: string; person_name?: string }
export type Category = { id: number; name: string; color: string; icon: string; sort_order: number }
export type Tag = { id: number; name: string; color: string }
export type EventType = { id: number; name: string; color: string; icon: string; is_default: boolean; sort_order: number }
export type Dashboard = { total_people: number; total_events: number; upcoming_days7: number; due_today: number; lend_fen: number; borrow_fen: number; pending_promises: number }
export type Suggestion = { type: string; person_id?: string; person_name?: string; message: string }
export type Relationship = { id: string; from_person_id: string; to_person_id: string; type: string; remark?: string; created_at: string; from_name?: string; to_name?: string }
export type TimelineItem = { date: string; type: string; title: string; person_id?: string; person_name?: string; id: string }

export const yuan = (fen: number) => (fen / 100).toFixed(2)
