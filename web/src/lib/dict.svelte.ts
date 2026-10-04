import { API, type Category, type Event, type EventType, type Tag } from './api'

// 全站共享的字典缓存。这些名单本来每次都一样：类别、标签、事件类型、关系类型
// 在往来的增删改里反复出现，翻页和切筛选还会再拉一轮。这里拉一次常驻，
// 只有真的改动了名单才由改动方显式刷新。
// 人物名单不在这里：它会长到成百上千，一律改成按需搜索（见 personSearch.ts）。
export const dict = $state({
  eventTypes: [] as EventType[],
  categories: [] as Category[],
  tags: [] as Tag[],
  relTypes: [] as string[],
  // 金钱表单里「关联到哪场往来」的下拉：只要最近这一小截
  events: [] as Event[],
})

const SOURCES = {
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

// 全量导入换掉的是整个库，谁都别再拿旧缓存。
export async function refreshAll() {
  await Promise.all((Object.keys(SOURCES) as DictName[]).map((n) => refresh(n)))
}
