// 以「我」为圆心的同心放射布局：圈层 = 关系图上离我几步，同圈按亲密度排。
// 力导向在只有几条关系时完全看不出中心，孤立点还会跟有连线的人混在一起。

const RING = 150 // 每多一步，半径至少增加这么多
const SPACING = 78 // 同圈两人之间的最小弧长，标签才不会叠在一起
const STAGGER = 44 // 每圈错开的起始角（度）：不错开的话单人链会叠成一条直线

export type EgoPos = { x: number; y: number; hop: number; angle: number }
export type EgoLayout = {
  pos: Map<string, EgoPos>
  radii: Map<number, number>
  maxHop: number
}

type Edge = { from_person_id: string; to_person_id: string }

export function egoLayout(selfId: string, edges: Edge[], order: (id: string) => number): EgoLayout {
  const adj = new Map<string, string[]>()
  const add = (a: string, b: string) => {
    if (a === b) return
    const list = adj.get(a)
    if (list) list.push(b)
    else adj.set(a, [b])
  }
  edges.forEach(e => { add(e.from_person_id, e.to_person_id); add(e.to_person_id, e.from_person_id) })

  const hops = new Map<string, number>([[selfId, 0]])
  const queue = [selfId]
  while (queue.length) {
    const cur = queue.shift()!
    for (const nb of adj.get(cur) ?? []) {
      if (hops.has(nb)) continue
      hops.set(nb, (hops.get(cur) ?? 0) + 1)
      queue.push(nb)
    }
  }

  const byHop = new Map<number, string[]>()
  for (const [id, k] of hops) {
    if (k === 0) continue
    const list = byHop.get(k)
    if (list) list.push(id)
    else byHop.set(k, [id])
  }

  const pos = new Map<string, EgoPos>([[selfId, { x: 0, y: 0, hop: 0, angle: 0 }]])
  const radii = new Map<number, number>()
  let prev = 0
  let maxHop = 0
  for (const k of [...byHop.keys()].sort((a, b) => a - b)) {
    const list = byHop.get(k)!.sort((a, b) => order(b) - order(a) || a.localeCompare(b, 'zh'))
    const radius = Math.max(prev + RING, (list.length * SPACING) / (2 * Math.PI))
    radii.set(k, radius)
    maxHop = k
    prev = radius
    const base = -90 + k * STAGGER
    list.forEach((id, i) => {
      const deg = base + (i + 0.5) * (360 / list.length)
      const rad = (deg * Math.PI) / 180
      pos.set(id, { x: radius * Math.cos(rad), y: radius * Math.sin(rad), hop: k, angle: rad })
    })
  }
  return { pos, radii, maxHop }
}

// 标签一律朝圆外推，挤在圆心的两侧会互相盖住
export function outwardLabel(angle: number): 'left' | 'right' | 'top' | 'bottom' {
  const c = Math.cos(angle), s = Math.sin(angle)
  if (c < -0.2) return 'left'
  if (c > 0.2) return 'right'
  return s < 0 ? 'top' : 'bottom'
}
