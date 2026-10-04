// 需要用户点头的地方统一走这里：原生 confirm 在手机上样式不可控、
// 默认焦点在「确定」上，肌肉记忆按回车就是一次不可恢复的删除。

export type AskOptions = {
  title: string
  detail?: string
  confirmLabel?: string
  cancelLabel?: string
  /** 删除/清空这类不可逆动作：按钮用红色，焦点默认落在「取消」上 */
  danger?: boolean
}

export type PendingAsk = AskOptions & { id: number; resolve: (ok: boolean) => void }

// 装在对象里导出：runes 只对属性响应，`export const` 的那个名字本身不能被重新赋值
export const askBox = $state<{ pending: PendingAsk | null }>({ pending: null })

let seq = 0

export function ask(options: AskOptions): Promise<boolean> {
  // 一次只弹一个：后开的直接按「取消」收掉，避免堆叠后分不清答的是哪个
  askBox.pending?.resolve(false)
  return new Promise<boolean>((resolve) => {
    askBox.pending = { ...options, id: ++seq, resolve }
  })
}

export function answerAsk(ok: boolean) {
  const cur = askBox.pending
  if (!cur) return
  askBox.pending = null
  cur.resolve(ok)
}
