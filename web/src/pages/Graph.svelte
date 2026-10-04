<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import * as echarts from 'echarts/core'
  import { GraphChart } from 'echarts/charts'
  import { TooltipComponent } from 'echarts/components'
  import { CanvasRenderer } from 'echarts/renderers'
  import { API, RELATION_TYPES, type Person, type Relationship, type Category, type Tag } from '../lib/api'
  import { navigate, route } from '../lib/router'
  import { self, loadSelf, personLabel } from '../lib/self.svelte'
  import { dict, ensure, refresh } from '../lib/dict.svelte'
  import {
    egoLayout, outwardLabel, circleFill, NO_CIRCLE_COLOR, edgePairKey, laneOf, pairKey,
    introEdgePairs, introChain, vennLayout, type VennCircle,
  } from '../lib/graphLayout'
  import { Plus, X, Link2, Route, Share2, Trash2 } from '@lucide/svelte'
  import PersonPicker from '../lib/PersonPicker.svelte'

  echarts.use([GraphChart, TooltipComponent, CanvasRenderer])

  type ViewMode = 'rel' | 'venn'

  let people = $state<Person[]>([])
  const categories = $derived(dict.categories)
  const allTags = $derived(dict.tags)
  let rels = $state<Relationship[]>([])
  let tagsByPerson = $state<Record<string, Tag[]>>({})
  const usedTypes = $derived(dict.relTypes)
  let chartDiv: HTMLDivElement | null = $state(null)
  let chart: echarts.ECharts | null = null
  // 容器尺寸变化（侧栏折叠、手机转屏、初始化时还没布局完）时让 ECharts 跟着重排，
  // 只监听 window resize 的话，零宽时刻 init 出来的图会一直是一条缝
  let ro: ResizeObserver | null = null
  // 圈层标尺只画连得上的人，剩下的人得说清楚去哪找
  let orphans = $state(0)
  let centered = $state(false)
  // 整图一次只取 500 个节点，超出部分后端会如实报截断，不能让人以为人丢了
  let graphMeta = $state({ truncated: false, total: 0, droppedEdges: 0 })

  let view = $state<ViewMode>('rel')
  // 关系视图筛选：空数组=不筛。选了只把不在其中的压暗而不是摘掉，节点一摘布局就跳。
  let catFilter = $state<number[]>([])
  let tagFilter = $state<number[]>([])
  // 认识路径开关：打开后引荐链画虚线，点节点从「进详情」变为「聚焦此人路径」
  let showChains = $state(false)
  let focusId = $state($route.query.focus || '')

  // 圈子视图：必选 2 个圈子，最多 3 个；第 4 个起在 UI 上禁选
  let vennSel = $state<number[]>([])
  let vennEdges = $state(false)

  // 弹窗：rel=单条关系；chain=引荐链（我 —类型→ 人 —类型→ 人 …）
  let showForm = $state(false)
  let formMode = $state<'rel' | 'chain'>('rel')
  let pending = $state<{ from: string; to: string } | null>(null)
  let form = $state({ from_person_id: '', to_person_id: '', type: RELATION_TYPES[0], remark: '' })
  let chainRows = $state<{ type: string; personId: string; newName: string; isNew: boolean }[]>([
    { type: '同学', personId: '', newName: '', isNew: false },
  ])
  let busy = $state(false)
  let dragEndedAt = 0

  async function load() {
    // 圈子/标签/关系类型是全站共享字典，这里只保证「拿到」，不再各自重拉
    await Promise.all([ensure('categories'), ensure('tags'), ensure('relTypes')])
    const r = await API.get('/api/v1/relationships') as any
    await loadSelf()
    people = r.people
    rels = r.relationships
    tagsByPerson = r.tags ?? {}
    graphMeta = { truncated: !!r.truncated, total: r.total_people ?? 0, droppedEdges: r.dropped_edges ?? 0 }
    // 圈子可能刚被从设置里删掉，选择里留着死 id 会把维恩图选空
    vennSel = vennSel.filter(id => categories.some(c => c.id === id))
    catFilter = catFilter.filter(id => categories.some(c => c.id === id))
  }

  // chartDiv 要等 {#if} 走到有图的那一支才绑定上，load() 里同步画会拿到 null；
  // 交给 effect，数据或任一视图状态变化后重画。
  $effect(() => {
    // 把影响绘图的状态都读一遍，任何一个变了就重绘
    const ready = people.length > 0
    const canDraw = ready && (view === 'rel' || (view === 'venn' && vennSel.length >= 2))
    // 下面这些状态虽不参与分支判断，也得显式读取以建立依赖
    void catFilter.length; void tagFilter.length; void showChains; void focusId
    void vennSel.length; void vennEdges
    if (chartDiv && canDraw) draw()
  })

  $effect(() => {
    // 浏览器前进后退 / 详情页深链过来时同步聚焦人
    focusId = $route.query.focus || ''
    if (focusId) view = 'rel'
  })

  const typeOptions = $derived(
    Array.from(new Set([...RELATION_TYPES, ...usedTypes, ...rels.map(r => r.type).filter(Boolean)])))

  // 维恩布局算一次：绘图和「圈外人物」chips 共用，别在模板里每渲染一次就松弛 240 轮
  const vennCircles = $derived<VennCircle[]>(
    vennSel
      .map(id => categories.find(c => c.id === id))
      .filter((c): c is Category => !!c)
      .map(c => ({ id: c.id, name: c.name, color: c.color })))
  const venn = $derived(view === 'venn' && vennCircles.length >= 2 ? vennLayout(vennCircles, people) : null)

  const dimByFilter = (p: Person) => {
    if (catFilter.length > 0 && !(p.categories ?? []).some(c => catFilter.includes(c.id))) return true
    if (tagFilter.length > 0 && !(tagsByPerson[p.id] ?? []).some(t => tagFilter.includes(t.id))) return true
    return false
  }

  function hexA(hex: string, alpha: number): string {
    const m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(hex.trim())
    if (!m) return hex
    let h = m[1]
    if (h.length === 3) h = h.split('').map(ch => ch + ch).join('')
    const n = parseInt(h, 16)
    return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${alpha})`
  }

  function draw() {
    if (!chartDiv) return
    chart?.dispose()
    ro?.disconnect()
    chart = echarts.init(chartDiv)
    ro = new ResizeObserver(() => chart?.resize())
    ro.observe(chartDiv)
    if (view === 'venn') drawVenn()
    else drawRel()
    chart.on('click', (p: any) => {
      if (Date.now() - dragEndedAt < 300) return
      if (p?.dataType !== 'node' || !p?.data?.id || p.data.guide || p.data.vennBg) return
      const id = p.data.id as string
      if (view === 'venn') { navigate(`/people/${id}`); return }
      // 认识路径模式下点节点 = 聚焦此人；否则进详情维护关系
      if (showChains) navigate(`/graph?focus=${encodeURIComponent(id)}`)
      else navigate(`/people/${id}`)
    })
  }

  function drawRel() {
    // 端点被归档的人不在 nodes 里，那条边会指向不存在的节点，得先滤掉
    const ids = new Set(people.map(p => p.id))
    const edges = rels.filter(r => ids.has(r.from_person_id) && ids.has(r.to_person_id))
    // 本人用琥珀色而不是主题色：跟其他节点同色就谈不上高亮了
    const accent = '#f59e0b'
    const gradeOf = new Map(people.map(p => [p.id, p.grade]))
    // 没设本人就无所谓「离我几步」；本人被归档时也一样，退回原来的力导向
    const layout = self.id && ids.has(self.id) ? egoLayout(self.id, edges, id => gradeOf.get(id) ?? 0) : null
    const shown = layout ? people.filter(p => layout.pos.has(p.id)) : people
    orphans = layout ? people.length - shown.length : 0
    centered = !!layout

    // 认识路径：聚焦时只留链上节点与边；开关打开但未聚焦时，所有引荐边虚线衬出
    const allIntroPairs = introEdgePairs(people)
    let chainIds: Set<string> | null = null
    let chainPairs: Set<string> | null = null
    if (focusId) {
      const chain = self.id ? introChain(self.id, focusId, people, rels) : null
      if (chain && (chain.reachesSelf || chain.chainIds.length > 1)) {
        chainIds = new Set(chain.chainIds)
        chainPairs = new Set()
        for (let i = 0; i < chain.chainIds.length - 1; i++) {
          chainPairs.add(pairKey(chain.chainIds[i], chain.chainIds[i + 1]))
        }
      }
    }
    const focusPair = focusId && self.id ? pairKey(self.id, focusId) : ''

    const dimOf = (p: Person) => {
      if (dimByFilter(p)) return true
      if (chainIds && !chainIds.has(p.id)) return true
      return false
    }

    const nodes: any[] = shown.map(p => {
      const me = p.id === self.id
      const at = layout?.pos.get(p.id)
      const dim = dimOf(p)
      const onChain = !!chainIds?.has(p.id)
      return {
        id: p.id, name: me ? `我 · ${p.name}` : p.name,
        ...(at && { x: at.x, y: at.y }),
        symbolSize: me ? 46 : 18 + p.grade * 4,
        label: {
          position: at ? (me ? 'bottom' : outwardLabel(at.angle)) : 'bottom',
          ...(me && { fontWeight: 'bold' }),
          ...(dim && { opacity: 0.15 }),
          ...(onChain && !me && { color: accent, fontWeight: 600 }),
        },
        itemStyle: me
          ? { color: accent, borderColor: '#fff', borderWidth: 2 }
          : {
              color: circleFill((p.categories ?? []).map(c => c.color || NO_CIRCLE_COLOR)),
              ...(onChain && { borderColor: accent, borderWidth: 2 }),
              ...(dim && { opacity: 0.12 }),
            },
      }
    })
    if (layout) {
      // 圈层标尺：symbol 用 'none' 时 echarts 连标签一起吞了，所以留个小点
      for (const [k, r] of layout.radii) {
        nodes.push({ id: `hop-${k}`, name: `${k} 步`, x: 0, y: -r, symbolSize: 5, draggable: false, guide: true,
          itemStyle: { color: 'rgba(148,163,184,0.35)' }, label: { show: true, position: 'right', fontSize: 11, color: '#94a3b8' } })
      }
    }
    const dimIds = new Set(shown.filter(dimOf).map(p => p.id))
    // 一对人现在可以并存几条不同类型的边，echarts 会把它们画成同一条线，标签叠死。
    // 按无序人对分组，同组的边轮流分到弯曲车道上；只有一条时留空，用系列默认的 0.1。
    const pairCount = new Map<string, number>()
    for (const r of edges) {
      const k = edgePairKey(r)
      pairCount.set(k, (pairCount.get(k) ?? 0) + 1)
    }
    const pairUsed = new Map<string, number>()
    const links = edges.map(r => {
      const pk = edgePairKey(r)
      const mine = !!self.id && (r.from_person_id === self.id || r.to_person_id === self.id)
      const onChain = chainPairs?.has(pk)
      const isDirectFocus = !!focusPair && pk === focusPair
      const dim = dimIds.has(r.from_person_id) || dimIds.has(r.to_person_id)
      const i = pairCount.get(pk)! > 1 ? (pairUsed.get(pk) ?? 0) : -1
      pairUsed.set(pk, i + 1)
      const curve = laneOf(i, r.from_person_id <= r.to_person_id)
      // 链边琥珀虚线；我与聚焦人的直达边琥珀实线（可能与链边重合，重合时按链边画）
      const chainEdge = showChains && allIntroPairs.has(pk)
      let color = mine ? accent : '#94a3b8'
      let opacity = mine ? 1 : 0.3
      let type: 'solid' | 'dashed' = 'solid'
      let width = mine ? 2.5 : 1
      if (onChain) { color = accent; opacity = 1; type = 'dashed'; width = 2.5 }
      else if (isDirectFocus) { color = accent; opacity = 1; width = 2.5 }
      else if (!focusId && chainEdge) { color = '#d97706'; opacity = 0.85; type = 'dashed'; width = 1.6 }
      if (dim) opacity = 0.04
      return {
        source: r.from_person_id, target: r.to_person_id,
        lineStyle: { ...(curve !== undefined && { curveness: curve }), color, width, opacity, type },
        label: {
          show: !dim,
          formatter: r.type, fontSize: 10,
          color: onChain || isDirectFocus || mine ? accent : '#94a3b8',
        },
      }
    })
    chart!.setOption({
      tooltip: { show: false },
      series: [{
        type: 'graph', layout: layout ? 'none' : 'force', roam: true, draggable: true,
        editable: true,
        ...(layout
          ? { left: 'center', top: 'middle' }
          : { force: { repulsion: 250, edgeLength: 120 }, left: '8%', right: '8%', top: '10%', bottom: '8%' }),
        emphasis: { focus: 'adjacency', lineStyle: { width: 3 } },
        lineStyle: { color: 'source', curveness: 0.1, opacity: 0.7 },
        label: { show: true, fontSize: 12, position: 'bottom' },
        edgeLabel: { position: 'middle' },
        data: nodes, links,
      }],
    })
  }

  function drawVenn() {
    const selCircles = vennCircles
    const layout = venn
    if (!layout || selCircles.length < 2) return
    const placed = new Set(layout.pos.keys())

    // 背景大圆必须排在人物节点前面：同一系列按 data 顺序绘制，先画的被压在下面
    const nodes: any[] = layout.circles.map((z, i) => ({
      id: `venn-circle-${selCircles[i].id}`,
      name: selCircles[i].name,
      x: z.cx, y: z.cy,
      symbolSize: z.r * 2,
      symbol: 'circle',
      draggable: false,
      vennBg: true,
      itemStyle: {
        color: hexA(selCircles[i].color, 0.09),
        borderColor: hexA(selCircles[i].color, 0.75),
        borderWidth: 2,
      },
      label: {
        show: true, position: [0, -z.r + 26], color: selCircles[i].color,
        fontSize: 15, fontWeight: 700,
      },
    }))

    for (const p of people) {
      const at = layout.pos.get(p.id)
      if (!at) continue
      const me = p.id === self.id
      const dim = dimByFilter(p)
      nodes.push({
        id: p.id, name: me ? `我 · ${p.name}` : p.name,
        x: at.x, y: at.y,
        symbolSize: me ? 46 : 18 + p.grade * 4,
        label: { show: true, position: 'bottom', fontSize: 12, ...(me && { fontWeight: 'bold' }), ...(dim && { opacity: 0.2 }) },
        itemStyle: me
          ? { color: '#f59e0b', borderColor: '#fff', borderWidth: 2 }
          : { color: circleFill((p.categories ?? []).map(c => c.color || NO_CIRCLE_COLOR)), ...(dim && { opacity: 0.15 }) },
      })
    }

    let links: any[] = []
    if (vennEdges) {
      links = rels
        .filter(r => placed.has(r.from_person_id) && placed.has(r.to_person_id))
        .map(r => ({
          source: r.from_person_id, target: r.to_person_id,
          lineStyle: { curveness: 0, opacity: 0.18, color: '#94a3b8' },
          label: { show: false },
        }))
    }

    chart!.setOption({
      tooltip: { show: false },
      series: [{
        type: 'graph', layout: 'none', roam: true, draggable: false,
        left: 'center', top: 'middle',
        emphasis: { focus: 'adjacency' },
        label: { show: false },
        data: nodes, links,
      }],
    })
    orphans = 0
    centered = false
  }

  function toggleCat(id: number) {
    catFilter = catFilter.includes(id) ? catFilter.filter(x => x !== id) : [...catFilter, id]
  }
  function toggleTag(id: number) {
    tagFilter = tagFilter.includes(id) ? tagFilter.filter(x => x !== id) : [...tagFilter, id]
  }
  function toggleVenn(id: number) {
    if (vennSel.includes(id)) vennSel = vennSel.filter(x => x !== id)
    else if (vennSel.length < 3) vennSel = [...vennSel, id]
  }

  function openForm(mode: 'rel' | 'chain' = 'rel', from = '', to = '') {
    formMode = mode
    if (!from && !to) from = self.id
    form = { from_person_id: from, to_person_id: to, type: RELATION_TYPES[0], remark: '' }
    pending = from && to ? { from, to } : null
    if (mode === 'chain') {
      chainRows = [{ type: '同学', personId: '', newName: '', isNew: false }]
    }
    showForm = true
  }
  function closeForm() {
    showForm = false
    pending = null
  }

  async function submitRel() {
    if (!form.from_person_id || !form.to_person_id) { alert('请选择关系的双方'); return }
    if (form.from_person_id === form.to_person_id) { alert('不能与自己建立关系'); return }
    if (!form.type.trim()) { alert('请填写关系类型'); return }
    busy = true
    try {
      await API.post('/api/v1/relationships', { ...form, type: form.type.trim() })
      closeForm()
      // 关系类型可能是现场敲的新词，字典得知道
      await Promise.all([load(), refresh('relTypes')])
    } catch (err: any) {
      alert('添加失败：' + (err?.message || err))
    } finally { busy = false }
  }

  function addChainRow() {
    chainRows = [...chainRows, { type: '', personId: '', newName: '', isNew: false }]
  }
  function removeChainRow(i: number) {
    chainRows = chainRows.filter((_, idx) => idx !== i)
  }
  // 每一跳要么选一个已有的人，要么当场新建；两种输入不能同时留着
  function toggleChainNew(i: number) {
    chainRows = chainRows.map((r, idx) => idx !== i ? r
      : { ...r, isNew: !r.isNew, personId: r.isNew ? r.personId : '', newName: r.isNew ? '' : r.newName })
  }

  // 引荐链提交：缺人的先建（仅姓名，引荐人=上一跳），再逐跳建边。
  // 已有人物不覆盖其引荐人；边已存在（409）视为跳过。中途失败不回滚，提示断点。
  async function submitChain() {
    if (!self.id) { alert('请先在人物详情里把自己设为「本人」'); return }
    const rows = chainRows.filter(r => r.type.trim() && ((r.isNew ? r.newName.trim() : r.personId)))
    if (rows.length === 0) { alert('至少填写一跳：关系类型 + 人物'); return }
    busy = true
    const failures: string[] = []
    let prevId = self.id
    try {
      for (let i = 0; i < rows.length; i++) {
        const row = rows[i]
        let pid = row.personId
        const isNew = row.isNew
        if (isNew) {
          // 第一跳与我直达，不设引荐人；其后每人的引荐人都是上一跳
          const body: any = { name: row.newName.trim() }
          if (prevId !== self.id) body.introduced_by_person_id = prevId
          const created = await API.post<Person>('/api/v1/people', body)
          pid = created.id
        } else if (prevId !== self.id) {
          // 已有人物：仅在还没记录引荐人时补上，不覆盖。
          // 图上只画了前 500 人，搜到的人可能不在这一页里，取不到就按 id 回查。
          const existing = people.find(p => p.id === pid)
            ?? await API.get<{ person: Person }>(`/api/v1/people/${pid}`).then(r => r.person).catch(() => undefined)
          if (existing && !existing.introduced_by_person_id) {
            const full: any = {
              ...existing,
              category_ids: (existing.categories ?? []).map(c => c.id),
              introduced_by_person_id: prevId,
            }
            await API.put(`/api/v1/people/${pid}`, full).catch((e: any) =>
              failures.push(`补「${existing.name}」引荐人失败：${e?.message || e}`))
          }
        }
        try {
          await API.post('/api/v1/relationships', {
            from_person_id: prevId, to_person_id: pid, type: row.type.trim(), remark: '',
          })
        } catch (e: any) {
          const msg = e?.message || String(e)
          if (!String(msg).includes('已经有同名')) failures.push(`第 ${i + 1} 跳关系创建失败：${msg}`)
        }
        prevId = pid
      }
      closeForm()
      // 链上可能敲了新关系类型，字典要跟上
      await Promise.all([load(), refresh('relTypes')])
      if (failures.length) alert('部分环节未成功：\n' + failures.join('\n'))
    } finally {
      busy = false
    }
  }

  const outsideChips = $derived(venn
    ? people.filter(p => !venn.pos.has(p.id))
    : [])

  function onResize() { chart?.resize() }
  onMount(() => { load(); window.addEventListener('resize', onResize) })
  onDestroy(() => { chart?.dispose(); ro?.disconnect(); window.removeEventListener('resize', onResize) })
</script>

<div class="space-y-4">
  <header class="flex flex-wrap items-start justify-between gap-3">
    <div>
      <h1 class="text-2xl font-semibold">关系图</h1>
      <p class="text-sm mt-1" style="color: var(--q-muted);">
        {#if view === 'rel'}
          {#if centered}圈层 = 关系上离我几步，同圈按亲密度排 · 虚线是「通过谁认识」的引荐链{:else}在人物详情里设为本人后，图会以你为中心按亲密度铺开{/if}
        {:else}
          圈子是集合：同时属于两个圈子的人落在交集里 · 选 2-3 个圈子对比
        {/if}
      </p>
    </div>
    <div class="flex items-center gap-2">
      <div class="flex rounded-lg p-0.5 text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);">
        <button class="px-3 py-1.5 rounded-md transition flex items-center gap-1"
                style={view === 'rel' ? 'background: var(--q-surface); color: var(--q-text);' : 'color: var(--q-muted);'}
                onclick={() => (view = 'rel')}>
          <Share2 size={14} /> 关系
        </button>
        <button class="px-3 py-1.5 rounded-md transition flex items-center gap-1"
                style={view === 'venn' ? 'background: var(--q-surface); color: var(--q-text);' : 'color: var(--q-muted);'}
                onclick={() => (view = 'venn')}>
          <Link2 size={14} /> 圈子
        </button>
      </div>
      <button class="flex items-center gap-1 px-3 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);"
              onclick={() => openForm('rel')}>
        <Plus size={14} /> 添加关系
      </button>
    </div>
  </header>

  {#if people.length === 0}
    <div class="text-center py-12 text-sm" style="color: var(--q-muted);">还没有人物或关系</div>
  {:else}
    {#if view === 'rel'}
      <div class="flex flex-wrap items-center gap-2">
        {#if categories.length > 0}
          <span class="text-xs mr-1" style="color: var(--q-muted);">按圈子筛</span>
          {#each categories as c}
            <button class="text-xs px-2 py-1 rounded-full transition"
                    style={catFilter.includes(c.id)
                      ? `background: color-mix(in srgb, ${c.color} 22%, transparent); color: ${c.color}; border: 1px solid ${c.color};`
                      : 'background: var(--q-bg); color: var(--q-muted); border: 1px solid var(--q-border);'}
                    onclick={() => toggleCat(c.id)}>{c.name}</button>
          {/each}
        {/if}
        {#if allTags.length > 0}
          <span class="text-xs ml-2 mr-1" style="color: var(--q-muted);">标签</span>
          {#each allTags as t}
            <button class="text-xs px-2 py-1 rounded-full transition"
                    style={tagFilter.includes(t.id)
                      ? `background: color-mix(in srgb, ${t.color} 22%, transparent); color: ${t.color}; border: 1px solid ${t.color};`
                      : 'background: var(--q-bg); color: var(--q-muted); border: 1px solid var(--q-border);'}
                    onclick={() => toggleTag(t.id)}>#{t.name}</button>
          {/each}
        {#if catFilter.length > 0 || tagFilter.length > 0}
          <button class="text-xs px-2 py-1 rounded-full underline" style="color: var(--q-muted);"
                  onclick={() => { catFilter = []; tagFilter = [] }}>清除</button>
        {/if}
        {/if}
        <label class="ml-auto flex items-center gap-1.5 text-xs cursor-pointer select-none" style="color: var(--q-muted);">
          <Route size={14} />
          <input type="checkbox" bind:checked={showChains} />
          认识路径（开启后点节点聚焦）
        </label>
        {#if focusId}
          {@const fp = people.find(p => p.id === focusId)}
          <button class="text-xs px-2 py-1 rounded-full"
                  style="background: color-mix(in srgb, #f59e0b 18%, transparent); color: #d97706; border: 1px solid #f59e0b;"
                  onclick={() => navigate('/graph')}>
            聚焦：{fp ? personLabel(fp) : '?'} ✕
          </button>
        {/if}
      </div>

      <div bind:this={chartDiv} style="width: 100%; height: 640px; border-radius: 12px; background: var(--q-surface); border: 1px solid var(--q-border);"></div>
      {#if graphMeta.truncated}
        <button class="w-full text-center text-sm py-2" style="color: var(--q-muted);" onclick={() => navigate('/people')}>
          通讯录共 {graphMeta.total} 人，图里只画前 {people.length} 人（按等级、最近更新排）
          {#if graphMeta.droppedEdges > 0}，另有 {graphMeta.droppedEdges} 条关系因端点未进图而暂未显示{/if} · 去通讯录
        </button>
      {/if}
      {#if centered && orphans > 0}
        <button class="w-full text-center text-sm py-2" style="color: var(--q-muted);" onclick={() => navigate('/people')}>
          另有 {orphans} 人还没有关系连线，未进图 · 去通讯录
        </button>
      {/if}
    {:else}
      {#if categories.length < 2}
        <div class="text-center py-12 text-sm space-y-2" style="color: var(--q-muted);">
          <p>至少需要两个圈子才能画交叠圈。</p>
          <button class="underline" onclick={() => navigate('/settings')}>去设置里建圈子</button>
        </div>
      {:else}
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-xs mr-1" style="color: var(--q-muted);">选圈子（2-3 个）</span>
          {#each categories as c}
            <button class="text-xs px-2 py-1 rounded-full transition disabled:opacity-40"
                    disabled={!vennSel.includes(c.id) && vennSel.length >= 3}
                    style={vennSel.includes(c.id)
                      ? `background: color-mix(in srgb, ${c.color} 22%, transparent); color: ${c.color}; border: 1px solid ${c.color};`
                      : 'background: var(--q-bg); color: var(--q-muted); border: 1px solid var(--q-border);'}
                    onclick={() => toggleVenn(c.id)}>{c.name}</button>
          {/each}
          <label class="ml-auto flex items-center gap-1.5 text-xs cursor-pointer select-none" style="color: var(--q-muted);">
            <input type="checkbox" bind:checked={vennEdges} />
            叠加关系连线
          </label>
        </div>

        {#if vennSel.length < 2}
          <div class="text-center py-16 text-sm" style="color: var(--q-muted);">请选择两个圈子查看交集</div>
        {:else}
          <div bind:this={chartDiv} style="width: 100%; height: 680px; border-radius: 12px; background: var(--q-surface); border: 1px solid var(--q-border);"></div>
          {#if outsideChips.length > 0}
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="text-xs mr-1" style="color: var(--q-muted);">不在所选圈子（{outsideChips.length}）</span>
              {#each outsideChips as p}
                <button class="text-xs px-2 py-1 rounded-full"
                        style="background: var(--q-bg); color: var(--q-muted); border: 1px solid var(--q-border);"
                        onclick={() => navigate(`/people/${p.id}`)}>{personLabel(p)}</button>
              {/each}
            </div>
          {/if}
        {/if}
      {/if}
    {/if}
  {/if}
</div>

{#if showForm}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4">
    <button type="button" aria-label="关闭弹窗" class="absolute inset-0 cursor-default" style="background: rgba(0,0,0,0.3); border: 0;" onclick={closeForm}></button>
    <div class="relative w-full max-w-lg rounded-2xl p-5 max-h-[85vh] overflow-y-auto" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold flex items-center gap-1"><Link2 size={16} /> {formMode === 'chain' ? '录入引荐链' : '添加关系'}</h2>
        <button onclick={closeForm}><X size={18} /></button>
      </div>

      <div class="flex rounded-lg p-0.5 text-sm mb-4 w-fit" style="background: var(--q-bg); border: 1px solid var(--q-border);">
        <button class="px-3 py-1 rounded-md" style={formMode === 'rel' ? 'background: var(--q-surface);' : 'color: var(--q-muted);'}
                onclick={() => (formMode = 'rel')}>单条关系</button>
        <button class="px-3 py-1 rounded-md flex items-center gap-1" style={formMode === 'chain' ? 'background: var(--q-surface);' : 'color: var(--q-muted);'}
                onclick={() => openForm('chain')}>
          <Route size={13} /> 引荐链
        </button>
      </div>

      {#if formMode === 'rel'}
        {#if !self.id}
          <p class="text-xs mb-3" style="color: var(--q-muted);">还没设「本人」，下面可以任选两人连线。</p>
        {/if}
        {#if pending}
          <div class="rounded-lg p-2 mb-3 text-sm flex items-center gap-2" style="background: color-mix(in srgb, var(--q-theme) 12%, transparent);">
            <span>{people.find(p => p.id === pending!.from)?.name || '?'}</span>
            <span style="color: var(--q-muted);">→</span>
            <span>{people.find(p => p.id === pending!.to)?.name || '?'}</span>
            <span class="text-xs ml-auto" style="color: var(--q-muted);">来自图上连线</span>
          </div>
        {/if}
        <div class="space-y-3">
          {#if pending}
            <p class="text-xs" style="color: var(--q-muted);">这一对来自图上的连线，直接填关系类型即可。</p>
          {:else}
            <div class="grid grid-cols-2 gap-3">
              <PersonPicker bind:value={form.from_person_id} exclude={[form.to_person_id]}
                            placeholder="甲方…" clearable={false} />
              <PersonPicker bind:value={form.to_person_id} exclude={[form.from_person_id]}
                            placeholder="乙方…" clearable={false} />
            </div>
          {/if}
          <input list="rel-type-options" bind:value={form.type} placeholder="关系类型（可自定义：闺蜜、对象、挚友…）"
                 class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" />
          <datalist id="rel-type-options">
            {#each typeOptions as t}<option value={t}></option>{/each}
          </datalist>
          <input bind:value={form.remark} placeholder="备注（可选）" class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" />
        </div>
        <div class="flex justify-end gap-2 mt-5">
          <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" onclick={closeForm}>取消</button>
          <button class="px-4 py-2 rounded-lg text-sm text-white disabled:opacity-60" style="background: var(--q-theme);" disabled={busy} onclick={submitRel}>保存</button>
        </div>
      {:else}
        {#if !self.id}
          <p class="text-sm mb-3" style="color: #dc2626;">请先在人物详情里把自己设为「本人」，再录入引荐链。</p>
        {/if}
        <p class="text-xs mb-3 leading-relaxed" style="color: var(--q-muted);">
          录「我 →同学→ A →对象→ B →闺蜜→ C」这样的认识经过。没有的人可以当场新建（只填名字），
          系统会给新人物自动记好引荐人；之后再补一条「我 —挚友→ C」的直达关系即可。
        </p>
        <div class="space-y-2">
          {#each chainRows as row, i}
            <div class="flex items-center gap-2">
              <span class="text-xs w-14 shrink-0 text-right" style="color: var(--q-muted);">{i === 0 ? '我' : '再经'}</span>
              <input list="rel-type-options" bind:value={row.type} placeholder="关系，如 同学/对象/闺蜜"
                     class="w-32 px-2 py-2 rounded-lg text-sm outline-none shrink-0" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" />
              <span style="color: var(--q-muted);">→</span>
              {#if row.isNew}
                <input bind:value={row.newName} placeholder="新人姓名" class="flex-1 min-w-0 px-2 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" />
              {:else}
                <div class="flex-1 min-w-0">
                  <PersonPicker bind:value={row.personId} placeholder="选择已有联系人…" clearable={false} />
                </div>
              {/if}
              <button class="px-2 py-1.5 rounded-lg text-xs shrink-0" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-muted);"
                      title={row.isNew ? '改为选择已有联系人' : '不选人，当场新建一个'}
                      onclick={() => toggleChainNew(i)}>
                {row.isNew ? '改选人' : '＋新建'}
              </button>
              {#if chainRows.length > 1}
                <button class="p-1 shrink-0" style="color: var(--q-muted);" onclick={() => removeChainRow(i)}><Trash2 size={14} /></button>
              {/if}
            </div>
          {/each}
          <datalist id="rel-type-options">
            {#each typeOptions as t}<option value={t}></option>{/each}
          </datalist>
          <button class="flex items-center gap-1 text-xs px-2 py-1 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-muted);" onclick={addChainRow}>
            <Plus size={12} /> 加一跳
          </button>
        </div>
        <div class="flex justify-end gap-2 mt-5">
          <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" onclick={closeForm}>取消</button>
          <button class="px-4 py-2 rounded-lg text-sm text-white disabled:opacity-60" style="background: var(--q-theme);" disabled={busy || !self.id} onclick={submitChain}>保存整条链</button>
        </div>
      {/if}
    </div>
  </div>
{/if}
