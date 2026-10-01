// 以「我」为圆心的同心放射布局：圈层 = 关系图上离我几步，同圈按亲密度排。
// 力导向在只有几条关系时完全看不出中心，孤立点还会跟有连线的人混在一起。

import type { Person, Relationship } from './api'

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

/** 无序人对的规范 key：边集合与认识链集合共用，才能互相对上 */
export function pairKey(a: string, b: string): string {
  return [a, b].sort().join('\u0000')
}

export function edgePairKey(r: Edge): string {
  return pairKey(r.from_person_id, r.to_person_id)
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

// ===== 认识链（引荐人路径） =====

/** 无序人对 → 两人之间全部边的类型，认识链每一跳拿它标注「这层是什么关系」 */
export function relTypeMap(rels: Relationship[]): Map<string, string[]> {
  const m = new Map<string, string[]>()
  for (const r of rels) {
    const k = pairKey(r.from_person_id, r.to_person_id)
    const list = m.get(k)
    if (list) {
      if (!list.includes(r.type)) list.push(r.type)
    } else {
      m.set(k, [r.type])
    }
  }
  return m
}

export function pairTypesOf(m: Map<string, string[]>, a: string, b: string): string[] {
  return m.get(pairKey(a, b)) ?? []
}

export type IntroChain = {
  /** 从根到本人的人物 id 序列；reachesSelf 时根就是「我」 */
  chainIds: string[]
  /** 长度 n-1：chainIds[i] → chainIds[i+1] 之间的边类型（无类型则空串） */
  edgeTypes: string[]
  reachesSelf: boolean
  /** 引荐人已被删除/不存在（外键正常会 SET NULL，兜底） */
  broken: boolean
  /** 数据里出现了引荐人环（写入时已拦，历史脏数据兜底） */
  cyclic: boolean
}

/**
 * 沿 introduced_by_person_id 从 personId 往回走，还原认识路径。
 * 深度由 seen 天然封顶（环或回到本人都终止）。
 */
export function introChain(selfId: string, personId: string, people: Person[], rels: Relationship[]): IntroChain {
  const byId = new Map(people.map(p => [p.id, p]))
  const types = relTypeMap(rels)
  const walked: string[] = []
  const seen = new Set<string>()
  let cur: string | undefined = personId
  let reachesSelf = false
  let broken = false
  while (cur && !seen.has(cur)) {
    seen.add(cur)
    walked.push(cur)
    if (cur === selfId) { reachesSelf = true; break }
    // 显式标注：循环里 cur 会被回写成 next，不标注 tsgo 会按 next→cur→next 报循环推断
    const next: string | undefined = byId.get(cur)?.introduced_by_person_id
    if (next) {
      if (!byId.has(next)) { broken = true; break }
      cur = next
    } else {
      cur = undefined
    }
  }
  const cyclic = !!(cur && seen.has(cur) && cur !== selfId)
  const chainIds = walked.reverse()
  const edgeTypes: string[] = []
  for (let i = 0; i < chainIds.length - 1; i++) {
    edgeTypes.push(pairTypesOf(types, chainIds[i], chainIds[i + 1]).join('、'))
  }
  return { chainIds, edgeTypes, reachesSelf, broken, cyclic }
}

/** 所有人引荐链涉及的边（无序 pair 集合），全图模式下把这些边统一画成虚线 */
export function introEdgePairs(people: Person[]): Set<string> {
  const out = new Set<string>()
  for (const p of people) {
    if (p.introduced_by_person_id) out.add(pairKey(p.id, p.introduced_by_person_id))
  }
  return out
}

// ===== 维恩/欧拉图布局（2-3 个圈子） =====
//
// 圈子是集合：等半径大圆，按所选圈子算归属掩码（2 圈 4 区 / 3 圈 8 区），
// 人从所在区的质心出发做松弛：节点互斥 + 回拉质心 + 圆内/圆外约束投影，
// 保证「仅同学」的人不出现在同事圈内，双圈者落在交集透镜里。

export type VennCircle = { id: number; name: string; color: string }
export type VennPos = { x: number; y: number; mask: number }
export type VennZone = { cx: number; cy: number; r: number }
export type VennLayout = {
  circles: VennZone[]
  pos: Map<string, VennPos>
  /** 不属于所选任一圈子的人，交给 UI 在图外列 chips */
  outside: string[]
}

type VennPerson = Pick<Person, 'id' | 'grade' | 'categories'>

const SAMPLE_STEP = 12 // 区域面积采样网格（px）
const RELAX_ITERS = 240
const CELL = 70 // 斥力空间哈希格子大小

function nodeRadius(p: VennPerson): number {
  return (18 + (p.grade || 0) * 4) / 2 + 5
}

function circleCenters(n: 2 | 3, d: number): { cx: number; cy: number }[] {
  if (n === 2) return [{ cx: -d / 2, cy: 0 }, { cx: d / 2, cy: 0 }]
  // 三个圆心成等边三角形，两两间距为 d
  return [0, 1, 2].map(k => {
    const ang = -Math.PI / 2 + (k * 2 * Math.PI) / 3
    const rho = d / Math.sqrt(3)
    return { cx: Math.cos(ang) * rho, cy: Math.sin(ang) * rho }
  })
}

type SampleBucket = { sx: number; sy: number; n: number }

/** 网格采样：统计每个归属掩码区域的面积（点数）与质心 */
function sampleZones(centers: { cx: number; cy: number }[], r: number) {
  const buckets = new Map<number, SampleBucket>()
  for (let x = -2 * r; x <= 2 * r; x += SAMPLE_STEP) {
    for (let y = -2 * r; y <= 2 * r; y += SAMPLE_STEP) {
      let mask = 0
      centers.forEach((c, i) => {
        if ((x - c.cx) ** 2 + (y - c.cy) ** 2 <= r * r) mask |= 1 << i
      })
      if (mask === 0) continue
      const b = buckets.get(mask)
      if (b) { b.sx += x; b.sy += y; b.n++ } else buckets.set(mask, { sx: x, sy: y, n: 1 })
    }
  }
  const zones = new Map<number, { x: number; y: number; area: number }>()
  for (const [mask, b] of buckets) {
    zones.set(mask, { x: b.sx / b.n, y: b.sy / b.n, area: b.n * SAMPLE_STEP * SAMPLE_STEP })
  }
  return zones
}

// 确定性伪随机：同一数据每次布局一致，避免节点每次跳动
function jitter(i: number, seed: number): number {
  const x = Math.sin(i * 127.1 + seed * 311.7) * 43758.5453
  return (x - Math.floor(x) - 0.5) * 24
}

export function vennLayout(circles: VennCircle[], people: VennPerson[]): VennLayout {
  const n = circles.length as 2 | 3
  const idxOf = new Map(circles.map((c, i) => [c.id, i]))
  const maskOf = (p: VennPerson) => {
    let mask = 0
    for (const c of p.categories ?? []) {
      const i = idxOf.get(c.id)
      if (i !== undefined) mask |= 1 << i
    }
    return mask
  }

  const groups = new Map<number, VennPerson[]>()
  const outside: string[] = []
  for (const p of people) {
    const mask = maskOf(p)
    if (mask === 0) { outside.push(p.id); continue }
    const g = groups.get(mask)
    if (g) g.push(p); else groups.set(mask, [p])
  }

  // 每区按打包密度 0.45 估算所需面积
  const need = new Map<number, number>()
  for (const [mask, list] of groups) {
    let area = 0
    for (const p of list) area += Math.PI * (nodeRadius(p) * 2) ** 2 * 0.45
    need.set(mask, area)
  }

  let r = 210
  let ratio = 1.12 // d / r
  let zones = new Map<number, { x: number; y: number; area: number }>()
  // 自适应圆心距：交集区挤就靠近，独占区挤就分开；比例到极限仍不够就放大半径
  for (let iter = 0; iter < 18; iter++) {
    const centers = circleCenters(n, ratio * r)
    zones = sampleZones(centers, r)
    let worst = 0
    let worstMask = 0
    for (const [mask, a] of need) {
      const have = zones.get(mask)?.area ?? 0
      const deficit = have > 0 ? a / have : 99
      if (deficit > worst) { worst = deficit; worstMask = mask }
    }
    if (worst <= 1.05) break
    const overlapZone = (worstMask & (worstMask - 1)) !== 0 // 至少两个 bit = 交集区
    if (overlapZone && ratio > 0.5) ratio -= 0.07
    else if (!overlapZone && ratio < 1.5) ratio += 0.05
    else if (worst > 1.25) { r *= 1.12; ratio = Math.min(ratio, 1.5) }
    if (r > 360) break
  }

  const d = ratio * r
  const centers = circleCenters(n, d)

  // 初始化到各区质心
  type Pt = { id: string; x: number; y: number; vr: number; mask: number }
  const pts: Pt[] = []
  for (const [mask, list] of groups) {
    const anchor = zones.get(mask)
    const ax = anchor?.x ?? 0
    const ay = anchor?.y ?? 0
    list.forEach((p, i) => {
      pts.push({ id: p.id, x: ax + jitter(i, mask), y: ay + jitter(i + 99, mask), vr: nodeRadius(p), mask })
    })
  }

  // 松弛：空间哈希做近邻斥力，质心回拉，圆内/圆外约束投影
  for (let iter = 0; iter < RELAX_ITERS; iter++) {
    const grid = new Map<string, Pt[]>()
    const keyOf = (x: number, y: number) => `${Math.floor(x / CELL)},${Math.floor(y / CELL)}`
    for (const p of pts) {
      const k = keyOf(p.x, p.y)
      const cell = grid.get(k)
      if (cell) cell.push(p); else grid.set(k, [p])
    }
    for (const p of pts) {
      const gx = Math.floor(p.x / CELL), gy = Math.floor(p.y / CELL)
      for (let gxx = gx - 1; gxx <= gx + 1; gxx++) {
        for (let gyy = gy - 1; gyy <= gy + 1; gyy++) {
          for (const q of grid.get(`${gxx},${gyy}`) ?? []) {
            if (q.id <= p.id) continue // 每对只处理一次
            const dx = p.x - q.x, dy = p.y - q.y
            const dist = Math.hypot(dx, dy) || 0.01
            const min = p.vr + q.vr + 3
            if (dist < min) {
              const push = (min - dist) / 2
              const ux = dx / dist, uy = dy / dist
              p.x += ux * push; p.y += uy * push
              q.x -= ux * push; q.y -= uy * push
            }
          }
        }
      }
      const anchor = zones.get(p.mask)
      if (anchor) {
        p.x += (anchor.x - p.x) * 0.03
        p.y += (anchor.y - p.y) * 0.03
      }
      // 约束投影：在圈内 / 在圈外（两轮，三圈时顺序投影可能互相顶来顶去）
      for (let pass = 0; pass < 2; pass++) {
        centers.forEach((c, i) => {
          const inside = (p.mask & (1 << i)) !== 0
          let dx = p.x - c.cx, dy = p.y - c.cy
          let dist = Math.hypot(dx, dy) || 0.01
          const limit = inside ? r - p.vr - 2 : r + p.vr + 2
          if (inside && dist > limit) {
            p.x = c.cx + (dx / dist) * limit
            p.y = c.cy + (dy / dist) * limit
          } else if (!inside && dist < limit) {
            // 恰在圆心时朝背离全局原点的方向推
            if (dist < 0.01) { dx = c.cx || 0.01; dy = c.cy || 0.01; dist = Math.hypot(dx, dy) || 0.01 }
            p.x = c.cx + (dx / dist) * limit
            p.y = c.cy + (dy / dist) * limit
          }
        })
      }
    }
  }

  const pos = new Map<string, VennPos>()
  for (const p of pts) pos.set(p.id, { x: p.x, y: p.y, mask: p.mask })
  return {
    circles: centers.map(c => ({ cx: c.cx, cy: c.cy, r })),
    pos,
    outside,
  }
}
