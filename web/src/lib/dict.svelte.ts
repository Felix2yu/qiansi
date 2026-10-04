import { API, type Category, type Event, type EventType, type Person, type Tag } from './api'

// 全站共享的字典缓存。这些名单本来每次都一样：往来页、对话页、金钱页、待办页、
// 纪念日页各自拉过 people?limit=500，翻页和切筛选还会再拉一轮。这里拉一次常驻，
// 只有真的改动了名单才由改动方显式刷新。
export const dict = $state({
  people: [] as Person[],
  eventTypes: [] as EventType[],
  categories: [] as Category[],
  tags: [] as Tag[],
  relTypes: [] as string[],
  // 金钱表单里「关联到哪场往来」的下拉：只要最近这一小截
  events: [] as Event[],
})

const ROSTER_LIMIT = 500

const SOURCES = {
  people: { key: 'people', path: `/api/v1/people?limit=${ROSTER_LIMIT}` },
  eventTypes: { key: 'eventTypes', path: '/api/v1/event-types' },
  categories: { key: 'categories', path: '/api/v1/categories' },
  tags: { key: 'tags', path: '/api/v1/tags' },
  relTypes: { key: 'relTypes', path: '/api/v1/relationships/types' },
  events: { key: 'events', path: '/api/v1/events?limit=100' },
} as const

export type DictName = keyof typeof SOURCES

const fresh = new Set<DictName>()
// 同一个字典在被填满之前会有好几个调用点等着它（页面 mount + 表单打开），
// 所以并发请求必须共用一趟。
const inflight = new Map<DictName, Promise<void>>()

function fetchInto(name: DictName): Promise<void> {
  const src = SOURCES[name]
  return (async () => {
    const list = await API.get<any[]>(src.path)
    dict[src.key] = list || []
    fresh.add(name)
  })().catch(() => {})
}

// ensure 只在没缓存时发请求；页面每次进入都调用它，命中的就是本地数据。
export function ensure(name: DictName): Promise<void> {
  if (fresh.has(name)) return Promise.resolve()
  const cur = inflight.get(name)
  if (cur) return cur
  const p = fetchInto(name).finally(() => {
    if (inflight.get(name) === p) inflight.delete(name)
  })
  inflight.set(name, p)
  return p
}

// refresh 是改动方用的：名单确实变了才重新拉，并且等拉完再返回，
// 免得调用方接着渲染还是旧的。
export function refresh(name: DictName): Promise<void> {
  fresh.delete(name)
  inflight.delete(name)
  return ensure(name)
}

export const ensurePeople = () => ensure('people')
export const refreshPeople = () => refresh('people')

// 全量导入换掉的是整个库，谁都别再拿旧缓存。
export async function refreshAll() {
  await Promise.all((Object.keys(SOURCES) as DictName[]).map((n) => refresh(n)))
}

export function personById(id: string): Person | undefined {
  return dict.people.find((p) => p.id === id)
}
