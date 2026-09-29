<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import * as echarts from 'echarts'
  import { API, RELATION_TYPES, type Person, type Relationship } from '../lib/api'
  import { navigate } from '../lib/router'
  import { Plus, X, Link2 } from '@lucide/svelte'

  let people = $state<Person[]>([])
  let rels = $state<Relationship[]>([])
  let chartDiv: HTMLDivElement | null = $state(null)
  let chart: echarts.ECharts | null = null

  // 表单：既用于「添加关系」按钮，也用于图上拖拽连线后补选类型
  let showForm = $state(false)
  let pending = $state<{ from: string; to: string } | null>(null)
  let form = $state({ from_person_id: '', to_person_id: '', type: RELATION_TYPES[0], remark: '' })
  let busy = $state(false)
  let dragEndedAt = 0

  async function load() {
    const r = await API.get('/api/v1/relationships') as any
    people = r.people
    rels = r.relationships
    draw()
  }

  function draw() {
    if (!chartDiv) return
    chart?.dispose()
    chart = echarts.init(chartDiv)
    const nodes = people.map(p => ({ id: p.id, name: p.name, category: p.category_id || 0, symbolSize: 14 + p.grade * 4 }))
    const links = rels.map(r => ({ source: r.from_person_id, target: r.to_person_id, label: { show: true, formatter: r.type, fontSize: 10, color: '#94a3b8' } }))
    chart.setOption({
      // editable 下 tooltip 会挡住拖拽的手柄，关掉
      tooltip: { show: false },
      legend: [{ data: Array.from(new Set(people.map(p => p.category_id || 0))).map(c => ({ name: '圈子' + c })) }],
      series: [{
        type: 'graph', layout: 'force', roam: true, draggable: true,
        // 从一个节点拖到另一个节点即发起连线，落下后由 graphEdge 事件接手
        editable: true,
        force: { repulsion: 250, edgeLength: 120 },
        emphasis: { focus: 'adjacency', lineStyle: { width: 3 } },
        lineStyle: { color: 'source', curveness: 0.1, opacity: 0.7 },
        label: { show: true, fontSize: 12 },
        edgeLabel: { position: 'middle' },
        data: nodes, links,
      }]
    })
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
      if (p?.dataType === 'node' && p?.data?.id) navigate(`/people/${p.data.id}`)
    })
  }

  function openForm(from = '', to = '') {
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
      <p class="text-sm mt-1" style="color: var(--q-muted);">从一个人物拖到另一个人物即可连线 · 点击节点进入详情维护关系</p>
    </div>
    <button class="flex items-center gap-1 px-3 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={() => openForm()}>
      <Plus size={14} /> 添加关系
    </button>
  </header>
  {#if people.length === 0}
    <div class="text-center py-12 text-sm" style="color: var(--q-muted);">还没有人物或关系</div>
  {:else}
    <div bind:this={chartDiv} style="width: 100%; height: 640px; border-radius: 12px; background: var(--q-surface); border: 1px solid var(--q-border);"></div>
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
            {#each people as p}<option value={p.id}>{p.name}</option>{/each}
          </select>
          <select bind:value={form.to_person_id} disabled={!!pending} class="w-full px-3 py-2 rounded-lg text-sm outline-none disabled:opacity-60" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
            <option value="">乙方…</option>
            {#each people as p}<option value={p.id}>{p.name}</option>{/each}
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
