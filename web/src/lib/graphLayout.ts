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

export function edgePairKey(r: Edge): string {
  return [r.from_person_id, r.to_person_id].sort().join('\u0000')
}

// 一对人可以并存好几条不同类型的边，全用同一个弯度就叠成一条线，标签互相盖住。
// 同一无序人对按出现次序轮流分车道：0、1、2… 对应 +/-0.15、+/-0.3、+/-0.45。
// curveness 是相对边的方向算的，反向的那条（b→a）取反后才会落到几何上的另一侧，
// 所以车道值要按「是否正序」翻一次，正反两条才不会各占半侧却仍然重合。
export function laneOf(i: number, canonical: boolean): number | undefined {
  if (i < 0) return undefined
  const magnitude = 0.15 * (1 + Math.floor(i / 2))
  const signed = i % 2 === 0 ? magnitude : -magnitude
  return canonical ? signed : -signed
}

export const NO_CIRCLE_COLOR = '#94a3b8'

// 一人可属多个圈子：节点按圈子顺序画硬边分段渐变（拼色），一段一个圈子。
// 不做颜色混合：圈子颜色是用户自定义的，混出来的第三个颜色既可能撞上某个
// 圈子的本色，也看不出这个人到底在哪几个圈里。
export function circleFill(colors: string[]) {
  const list = colors.length > 0 ? colors : [NO_CIRCLE_COLOR]
  const n = list.length
  const stops: { offset: number; color: string }[] = []
  list.forEach((color, i) => {
    // 交界处放两个相同 offset 的站点，ECharts 才给硬边而不是过渡
    stops.push({ offset: i / n, color })
    stops.push({ offset: (i + 1) / n, color })
  })
  return { type: 'linear', x: 0, y: 0, x2: 1, y2: 0, colorStops: stops }
}
