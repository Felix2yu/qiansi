import { API } from './api'

// 选人不再依赖「一次拉 500 人」的全量名单：输入即搜，命中多少算多少。
// 这里只做两件事——按关键词搜，以及把已经选中的 id 换成能显示的名字。

export type PersonLite = { id: string; name: string; nickname?: string }

const LIMIT = 20
const cache = new Map<string, PersonLite>()
const inflight = new Map<string, Promise<PersonLite | null>>()

export function cachedPerson(id: string): PersonLite | undefined {
  return cache.get(id)
}

function remember(p: PersonLite) {
  cache.set(p.id, p)
}

export async function searchPeople(q: string, exclude: string[] = []): Promise<PersonLite[]> {
  const qs = new URLSearchParams({ q, limit: String(LIMIT), archived: '1' })
  // 失败要让调用方看得见：静默返回空列表会被当成「没有这个人」
  const list = await API.get<PersonLite[]>(`/api/v1/people?${qs}`)
  const skip = new Set(exclude.filter(Boolean))
  for (const p of list || []) remember(p)
  return (list || []).filter(p => !skip.has(p.id))
}

// resolvePerson 给一个 id 取回可显示的姓名；同一次操作里重复问同一个 id 只发一个请求。
export function resolvePerson(id: string): Promise<PersonLite | null> {
  const hit = cache.get(id)
  if (hit) return Promise.resolve(hit)
  const pending = inflight.get(id)
  if (pending) return pending
  const p = (async () => {
    if (!id) return null
    try {
      const res = await API.get<{ person: PersonLite }>(`/api/v1/people/${id}`)
      const person = res.person
      if (person) remember(person)
      return person || null
    } catch {
      return null
    }
  })().finally(() => inflight.delete(id))
  inflight.set(id, p)
  return p
}

export async function resolvePeople(ids: string[]): Promise<PersonLite[]> {
  const got = await Promise.all(ids.map(resolvePerson))
  return got.filter((p): p is PersonLite => !!p)
}
