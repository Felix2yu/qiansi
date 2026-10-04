// 全站唯一的反馈通道。原生 alert 会卡住整个页面、移动端样式不可控，
// 而且一关闭就什么都不剩：写操作失败必须留在屏幕上，让人来得及截图重试。

export type ToastKind = 'ok' | 'error' | 'info'

export type ToastItem = {
  id: number
  kind: ToastKind
  text: string
  /** 有 undo 的那条会挂一个「撤销」按钮，5 秒后连同动作一起消失 */
  undoLabel?: string
  onUndo?: () => void | Promise<void>
}

export const toasts = $state<ToastItem[]>([])

const TTL: Record<ToastKind, number> = { ok: 2600, info: 3000, error: 6000 }
// 审计里定的窗口：操作后 5 秒内可以反悔
export const UNDO_MS = 5000

let seq = 0
const timers = new Map<number, ReturnType<typeof setTimeout>>()

function schedule(id: number, ms: number) {
  timers.set(id, setTimeout(() => dismiss(id), ms))
}

export function dismiss(id: number) {
  const t = timers.get(id)
  if (t) { clearTimeout(t); timers.delete(id) }
  const i = toasts.findIndex(x => x.id === id)
  if (i >= 0) toasts.splice(i, 1)
}

function push(kind: ToastKind, text: string, ms?: number, undo?: { label: string; run: () => void | Promise<void> }) {
  const item: ToastItem = { id: ++seq, kind, text }
  if (undo) { item.undoLabel = undo.label; item.onUndo = undo.run }
  toasts.push(item)
  // 只留最近几条，连续失败时不要把屏幕堆满
  while (toasts.length > 4) dismiss(toasts[0].id)
  schedule(item.id, ms ?? TTL[kind])
  return item.id
}

export const toast = {
  ok: (text: string) => push('ok', text),
  info: (text: string) => push('info', text),
  error: (text: string) => push('error', text),
  /** 写操作失败的统一出口：主语由调用方给（「保存失败」），宾语用服务端的人话 */
  fail: (action: string, err: unknown) => push('error', `${action}：${errText(err)}`),
  /** 可逆操作（归档、完成、改状态）用这条：5 秒内点「撤销」就回到原样 */
  undoable: (text: string, run: () => void | Promise<void>, label = '撤销') =>
    push('ok', text, UNDO_MS, { label, run }),
}

// fetch 抛的是 Error，服务端文案已经在 message 里；其余形态（字符串、undefined）也要能显示
export function errText(err: unknown): string {
  if (err instanceof Error) return err.message || '请求失败'
  const s = String(err ?? '')
  return s && s !== 'undefined' ? s : '未知错误'
}

export async function runUndo(item: ToastItem) {
  const id = item.id
  dismiss(id)
  await item.onUndo?.()
}
