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

  // 令牌模式下，<img src="/uploads/…"> 和「点链接直接下载」都带不上自定义请求头，
  // 只认令牌的鉴权会让头像裂图、导出 401。用一次带令牌的 POST 换一枚只读会话
  // cookie（服务端只让它放行 GET/HEAD），这些浏览器自发起的请求才走得通。
  async function ensureSession(): Promise<boolean> {
    if (!localStorage.getItem('q_token')) return true
    try {
      const res = await fetch(base + '/api/v1/auth/session', { method: 'POST', headers: authHeaders() })
      return res.ok
    } catch {
      return false
    }
  }

  // 下载走 fetch 而不是 <a href>：既能在令牌模式下显式带头，也能把服务端的错误
  // （令牌不对、快照失败）报回来，而不是让当前页跳成一个错误页、看起来像「已经导出了」。
  async function download(path: string, filename = ''): Promise<void> {
    const res = await fetch(base + path, { headers: authHeaders() })
    if (!res.ok) {
      let msg = res.statusText
      try { msg = (await res.json()).error || msg } catch {}
      throw new Error(msg)
    }
    const cd = res.headers.get('Content-Disposition') || ''
    const name = filename || cd.match(/filename="?([^";]+)"?/)?.[1] || 'download'
    const url = URL.createObjectURL(await res.blob())
    const a = document.createElement('a')
    a.href = url
    a.download = name
    document.body.appendChild(a)
    a.click()
    a.remove()
    // 马上 revoke 会掐断刚开始的下载，留出读取 blob 的时间
    setTimeout(() => URL.revokeObjectURL(url), 30000)
  }

  return {
    get: <T>(p: string) => req<T>(p),
    post: <T>(p: string, body: any) => req<T>(p, { method: 'POST', headers: headers(), body: JSON.stringify(body) }),
    put: <T>(p: string, body: any) => req<T>(p, { method: 'PUT', headers: headers(), body: JSON.stringify(body) }),
    delete: <T>(p: string, body?: any) => req<T>(p, { method: 'DELETE', ...(body ? { headers: headers(), body: JSON.stringify(body) } : {}) }),
    authHeaders,
    ensureSession,
    download,
  }
})()

export type Person = {
  id: string; name: string; family_name?: string; given_name?: string;
  nickname?: string; gender?: string;
  birthday?: string; birthday_is_lunar?: boolean; avatar_attachment_id?: string;
  phone?: string; wechat?: string; location?: string; notes?: string;
  grade: number; category_ids?: number[]; categories?: PersonCategory[]; archived?: boolean;
  created_at: string; updated_at: string;
  intimacy?: number;
  /** 认识来源：通过谁认识；空 = 与我直接认识 */
  introduced_by_person_id?: string;
  /** 仅详情接口带回引荐人姓名 */
  introduced_by_name?: string;
  /** 仅关系图接口批量带回 */
  tags?: Tag[];
}
export type Event = { id: string; title: string; type_id?: number; type_name?: string; type_color?: string; event_date: string; location?: string; locations?: string[]; has_gift?: boolean; gift?: string; summary?: string; created_at: string; updated_at: string; participants?: Person[]; expense_fen?: number; expense_person_id?: string; expenses?: Transaction[] }
export type Memo = { id: string; person_id?: string; speaker: string; content: string; said_at: string; is_promise: boolean; due_date?: string; status: string; created_at: string }
export type Transaction = { id: string; person_id: string; kind: string; direction: string; amount_fen: number; title?: string; occurred_at: string; due_date?: string; settled: boolean; settled_at?: string; created_at: string; person_name?: string; repaid_fen?: number; event_id?: string; event_title?: string }
export type Anniversary = { id: string; person_id?: string; title: string; date: string; is_lunar: boolean; repeat_yearly: boolean; remind_days: string; created_at: string; person_name?: string; next_date?: string; days_until?: number | null }
export type Reminder = { id: string; person_id?: string; ref_type: string; ref_id?: string; title: string; due_at: string; status: string; created_at: string; completed_at?: string; person_name?: string }
export type Category = { id: number; name: string; color: string; icon: string; sort_order: number }
/** 挂在人头上的圈子（读路径），颜色用来在列表和图谱上认圈子 */
export type PersonCategory = { id: number; name: string; color: string }
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
/** 自动备份配置（与后端 internal/backup.Schedule 一一对应） */
export type BackupSchedule = {
  enabled: boolean
  frequency: 'daily' | 'weekly' | 'custom'
  at: string          // "HH:MM"，daily/weekly 生效
  weekday: number     // 0=周日 … 6=周六，weekly 生效
  every_min: number   // 自定义间隔（分钟）
  keep: number        // 归档保留份数
}
/** 自动备份状态：配置 + 执行结果 */
export type BackupStatus = {
  schedule: BackupSchedule
  enabled: boolean
  label: string          // 周期中文描述
  next_run: string       // RFC3339，空串表示未排期
  next_run_in: string    // 倒计时
  next_hint: string      // 例如"每天 04:00 执行"
  last_run: string       // RFC3339，从未执行过则空串
  last_path: string
  last_error: string     // 上次失败原因
  keep: number
  backups_dir: string
}
export type TrendPoint = { day: string; score: number }
export type SearchResult = { type: string; id: string; title: string; subtitle?: string; date?: string; path: string }

// ===== 枚举本地化：后端存英文，界面统一显示中文 =====
export const KIND_LABEL: Record<string, string> = { loan: '借还', gift: '礼物', expense: '花销', other: '其它' }
export const DIRECTION_LABEL: Record<string, string> = { out: '我支出', in: '我收入' }
export const MEMO_STATUS_LABEL: Record<string, string> = { open: '进行中', fulfilled: '已兑现', broken: '未兑现' }
export const REF_TYPE_LABEL: Record<string, string> = { custom: '手动', anniversary: '纪念日', birthday: '生日', memo: '对话', transaction: '金钱', promise: '承诺' }
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
