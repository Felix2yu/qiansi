import { API } from './api'

// 单人使用、以我为中心：本人只是指向 people 某条记录的指针（settings.self_person_id），
// 所有选人下拉都把它排到第一位并标出来。
export const self = $state({ id: '' })

let inflight: Promise<void> | null = null
export function loadSelf(force = false): Promise<void> {
  if (!force && inflight) return inflight
  inflight = (async () => {
    const st = await API.get<Record<string, string>>('/api/v1/settings')
    self.id = st.self_person_id || ''
  })().catch(() => {})
  return inflight
}

export function setSelf(id: string) {
  self.id = id
}

export function isSelf(id: string) {
  return !!self.id && self.id === id
}

// selfFirst 把本人挪到列表最前，exclude 用于排除当前正在编辑的那个人
export function selfFirst<T extends { id: string }>(list: T[], exclude = ''): T[] {
  const rest = list.filter((p) => p.id !== exclude)
  if (!self.id) return rest
  const me = rest.find((p) => p.id === self.id)
  if (!me) return rest
  return [me, ...rest.filter((p) => p.id !== self.id)]
}

export function personLabel(p: { id: string; name: string }) {
  return isSelf(p.id) ? `我 · ${p.name}` : p.name
}
