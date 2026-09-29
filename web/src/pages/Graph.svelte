<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import * as echarts from 'echarts/core'
  import { GraphChart } from 'echarts/charts'
  import { LegendComponent, TooltipComponent } from 'echarts/components'
  import { CanvasRenderer } from 'echarts/renderers'
  import { API, RELATION_TYPES, type Person, type Relationship } from '../lib/api'
  import { navigate } from '../lib/router'
  import { self, loadSelf, selfFirst, personLabel } from '../lib/self.svelte'
  import { egoLayout, outwardLabel } from '../lib/graphLayout'
  import { Plus, X, Link2 } from '@lucide/svelte'

  echarts.use([GraphChart, LegendComponent, TooltipComponent, CanvasRenderer])

  let people = $state<Person[]>([])
  let rels = $state<Relationship[]>([])
  let chartDiv: HTMLDivElement | null = $state(null)
  let chart: echarts.ECharts | null = null
  // 放射布局只画连得上的人，剩下的人得说清楚去哪找
  let orphans = $state(0)
  let centered = $state(false)

  // 表单：既用于「添加关系」按钮，也用于图上拖拽连线后补选类型
  let showForm = $state(false)
  let pending = $state<{ from: string; to: string } | null>(null)
  let form = $state({ from_person_id: '', to_person_id: '', type: RELATION_TYPES[0], remark: '' })
  let busy = $state(false)
  let dragEndedAt = 0

  async function load() {
    const r = await API.get('/api/v1/relationships') as any
    await loadSelf()
    people = r.people
    rels = r.relationships
  }

  // chartDiv 要等 {#if} 走到有图的那一支才绑定上，load() 里同步画会拿到 null；
  // 交给 effect，数据或容器就绪后再画。
  $effect(() => {
    if (chartDiv && people.length > 0) draw()
  })

  function draw() {
    if (!chartDiv) return
    chart?.dispose()
    chart = echarts.init(chartDiv)
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
    // node.category 是 categories 的下标而不是圈子 id；少了 categories 时节点会整个不画，
    // 所以先把用到的圈子编号映射成连续下标，再按同一顺序生成 categories。
    const catIds = Array.from(new Set(shown.map(p => p.category_id || 0))).sort((a, b) => a - b)
    const catIndex = new Map(catIds.map((c, i) => [c, i]))
    const categories = catIds.map(c => ({ name: '圈子' + c }))
    const nodes: any[] = shown.map(p => {
      const me = p.id === self.id
      const at = layout?.pos.get(p.id)
      return {
        id: p.id, name: me ? `我 · ${p.name}` : p.name,
        category: catIndex.get(p.category_id || 0)!,
        ...(at && { x: at.x, y: at.y }),
        symbolSize: me ? 46 : 14 + p.grade * 4,
        // 圈上的标签一律朝外，否则全挤在圆心和彼此身上；我在圆心，朝下就好
        label: { position: at ? (me ? 'bottom' : outwardLabel(at.angle)) : 'bottom', ...(me && { fontWeight: 'bold' }) },
        ...(me && { itemStyle: { color: accent, borderColor: '#fff', borderWidth: 2 } }),
      }
    })
    if (layout) {
      // 圈层标尺：symbol 用 'none' 时 echarts 连标签一起吞了，所以留个小点
      for (const [k, r] of layout.radii) {
        nodes.push({ id: `hop-${k}`, name: `${k} 步`, x: 0, y: -r, symbolSize: 5, draggable: false, guide: true,
          itemStyle: { color: 'rgba(148,163,184,0.35)' }, label: { show: true, position: 'right', fontSize: 11, color: '#94a3b8' } })
      }
    }
    const links = edges.map(r => {
      const mine = !!self.id && (r.from_person_id === self.id || r.to_person_id === self.id)
      return {
        source: r.from_person_id, target: r.to_person_id,
        lineStyle: mine ? { color: accent, width: 2.5, opacity: 1 } : { opacity: 0.3 },
        label: { show: true, formatter: r.type, fontSize: 10, color: mine ? accent : '#94a3b8' },
      }
    })
    chart.setOption({
      // editable 下 tooltip 会挡住拖拽的手柄，关掉
      tooltip: { show: false },
      legend: [{ data: categories.map(c => c.name) }],
      series: [{
        type: 'graph', layout: layout ? 'none' : 'force', roam: true, draggable: true,
        // 从一个节点拖到另一个节点即发起连线
        editable: true,
        // 不锁比例的话放射布局会被拉伸成椭圆，圈层就看不出是圈了
        ...(layout ? { preserveAspect: true } : { force: { repulsion: 250, edgeLength: 120 } }),
        left: '8%', right: '8%', top: '10%', bottom: '8%',
        emphasis: { focus: 'adjacency', lineStyle: { width: 3 } },
        lineStyle: { color: 'source', curveness: 0.1, opacity: 0.7 },
        label: { show: true, fontSize: 12, position: 'bottom' },
        edgeLabel: { position: 'middle' },
        categories, data: nodes, links,
      }]
    })
    // 尚未生效：echarts 5/6 都没有 graphEdge 这个事件名，图上拖拽连线还没接通
    chart.on('graphEdge', (p: any) => {
      const to = p?.targetNode?.id
      const from = p?.targetNodeEdge?.source
      if (!to || !from) return
      dragEndedAt = Date.now()
      // 后端要求 type 非空，拖完先记下两端，再让用户补一个类型
      openForm(from, to)
    })
    // 点击节点进人物详情维护关系；刚拖完的那一下不跳转，免得误进
    chart.on('click', (p: any) => {
      if (Date.now() - dragEndedAt < 300) return
      if (p?.dataType === 'node' && p?.data?.id && !p.data.guide) navigate(`/people/${p.data.id}`)
    })
  }

  function openForm(from = '', to = '') {
    // 单人应用，从按钮新建的关系默认就是我跟别人
    if (!from && !to) from = self.id
    form = { from_person_id: from, to_person_id: to, type: RELATION_TYPES[0], remark: '' }
    pending = from && to ? { from, to } : null
    showForm = true
  }

  function closeForm() {
    showForm = false
    pending = null
  }

  async function submit() {
    if (!form.from_person_id || !form.to_person_id) { alert('请选择关系的双方'); return }
    if (form.from_person_id === form.to_person_id) { alert('不能与自己建立关系'); return }
    busy = true
    try {
      await API.post('/api/v1/relationships', { ...form })
      closeForm()
      await load()
    } catch (err: any) {
      alert('添加失败：' + (err?.message || err))
    } finally { busy = false }
  }

  function onResize() { chart?.resize() }
  onMount(() => { load(); window.addEventListener('resize', onResize) })
  onDestroy(() => { chart?.dispose(); window.removeEventListener('resize', onResize) })
</script>
<div class="space-y-4">
  <header class="flex flex-wrap items-start justify-between gap-3">
    <div>
      <h1 class="text-2xl font-semibold">关系图</h1>
      <p class="text-sm mt-1" style="color: var(--q-muted);">{#if centered}圈层 = 关系上离我几步，同圈按亲密度排 · 点击节点进详情维护关系{:else}在人物详情里设为本人后，图会以你为中心按亲密度铺开{/if}</p>
    </div>
    <button class="flex items-center gap-1 px-3 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={() => openForm()}>
      <Plus size={14} /> 添加关系
    </button>
  </header>
  {#if people.length === 0}
    <div class="text-center py-12 text-sm" style="color: var(--q-muted);">还没有人物或关系</div>
  {:else}
    <div bind:this={chartDiv} style="width: 100%; height: 640px; border-radius: 12px; background: var(--q-surface); border: 1px solid var(--q-border);"></div>
    {#if centered && orphans > 0}
      <button class="w-full text-center text-sm py-2" style="color: var(--q-muted);" onclick={() => navigate('/people')}>
        另有 {orphans} 人还没有关系连线，未进图 · 去通讯录
      </button>
    {/if}
  {/if}
</div>

{#if showForm}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4">
    <button type="button" aria-label="关闭弹窗" class="absolute inset-0 cursor-default" style="background: rgba(0,0,0,0.3); border: 0;" onclick={closeForm}></button>
    <div class="relative w-full max-w-md rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold flex items-center gap-1"><Link2 size={16} /> 添加关系</h2>
        <button onclick={closeForm}><X size={18} /></button>
      </div>
      {#if pending}
        <div class="rounded-lg p-2 mb-3 text-sm flex items-center gap-2" style="background: color-mix(in srgb, var(--q-theme) 12%, transparent);">
          <span>{people.find(p => p.id === pending!.from)?.name || '?'}</span>
          <span style="color: var(--q-muted);">→</span>
          <span>{people.find(p => p.id === pending!.to)?.name || '?'}</span>
          <span class="text-xs ml-auto" style="color: var(--q-muted);">来自图上连线</span>
        </div>
      {/if}
      <div class="space-y-3">
        <div class="grid grid-cols-2 gap-3">
          <select bind:value={form.from_person_id} disabled={!!pending} class="w-full px-3 py-2 rounded-lg text-sm outline-none disabled:opacity-60" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
            <option value="">甲方…</option>
            {#each selfFirst(people, form.to_person_id) as p}<option value={p.id}>{personLabel(p)}</option>{/each}
          </select>
          <select bind:value={form.to_person_id} disabled={!!pending} class="w-full px-3 py-2 rounded-lg text-sm outline-none disabled:opacity-60" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
            <option value="">乙方…</option>
            {#each selfFirst(people, form.from_person_id) as p}<option value={p.id}>{personLabel(p)}</option>{/each}
          </select>
        </div>
        <select bind:value={form.type} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
          {#each RELATION_TYPES as t}<option value={t}>{t}</option>{/each}
        </select>
        <input bind:value={form.remark} placeholder="备注（可选）" class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" />
      </div>
      <div class="flex justify-end gap-2 mt-5">
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" onclick={closeForm}>取消</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white disabled:opacity-60" style="background: var(--q-theme);" disabled={busy} onclick={submit}>保存</button>
      </div>
    </div>
  </div>
{/if}
