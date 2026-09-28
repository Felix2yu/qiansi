<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import * as echarts from 'echarts'
  import { API, type Person, type Relationship } from '../lib/api'

  let people = $state<Person[]>([])
  let rels = $state<Relationship[]>([])
  let chartDiv: HTMLDivElement | null = null
  let chart: echarts.ECharts | null = null

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
      tooltip: {},
      legend: [{ data: Array.from(new Set(people.map(p => p.category_id || 0))).map(c => ({ name: '圈子' + c })) }],
      series: [{
        type: 'graph', layout: 'force', roam: true, draggable: true,
        force: { repulsion: 250, edgeLength: 120 },
        emphasis: { focus: 'adjacency', lineStyle: { width: 3 } },
        lineStyle: { color: 'source', curveness: 0.1, opacity: 0.7 },
        label: { show: true, fontSize: 12 },
        edgeLabel: { position: 'middle' },
        data: nodes, links,
      }]
    })
  }

  function onResize() { chart?.resize() }
  onMount(() => { load(); window.addEventListener('resize', onResize) })
  onDestroy(() => { chart?.dispose(); window.removeEventListener('resize', onResize) })
</script>
<div class="space-y-4">
  <header><h1 class="text-2xl font-semibold">关系图</h1><p class="text-sm mt-1" style="color: var(--q-muted);">人物之间的联结</p></header>
  {#if people.length === 0}
    <div class="text-center py-12 text-sm" style="color: var(--q-muted);">还没有人物或关系</div>
  {:else}
    <div bind:this={chartDiv} style="width: 100%; height: 640px; border-radius: 12px; background: var(--q-surface); border: 1px solid var(--q-border);"></div>
  {/if}
</div>
