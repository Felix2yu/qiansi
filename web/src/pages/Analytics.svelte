<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import * as echarts from 'echarts/core'
  import { BarChart } from 'echarts/charts'
  import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
  import { CanvasRenderer } from 'echarts/renderers'
  import { API, type GradeDist } from '../lib/api'

  echarts.use([BarChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])

  let monthChartDiv: HTMLDivElement | null = null
  let gradeChartDiv: HTMLDivElement | null = null
  let monthChart: echarts.ECharts | null = null
  let gradeChart: echarts.ECharts | null = null

  async function load() {
    const [month, grade] = await Promise.all([
      API.get('/api/v1/dashboard/by-month'),
      API.get('/api/v1/dashboard/grade-distribution'),
    ]) as any
    drawMonth(month)
    drawGrade(grade)
  }
  function drawMonth(rows: any[]) {
    if (!monthChartDiv) return
    monthChart?.dispose()
    monthChart = echarts.init(monthChartDiv)
    monthChart.setOption({
      tooltip: {}, legend: { data: ['往来', '金钱', '对话'] },
      grid: { left: 40, right: 20, top: 40, bottom: 30 },
      xAxis: { type: 'category', data: rows.map(r => r.month) },
      yAxis: { type: 'value' },
      series: [
        { name: '往来', type: 'bar', data: rows.map(r => r.event_count), itemStyle: { color: '#10b981' } },
        { name: '金钱', type: 'bar', data: rows.map(r => r.tx_count), itemStyle: { color: '#ef4444' } },
        { name: '对话', type: 'bar', data: rows.map(r => r.memo_count), itemStyle: { color: '#f59e0b' } },
      ],
    })
  }
  function drawGrade(rows: GradeDist[]) {
    if (!gradeChartDiv) return
    gradeChart?.dispose()
    gradeChart = echarts.init(gradeChartDiv)
    const colors = ['#e2e8f0', '#93c5fd', '#60a5fa', '#3b82f6', '#1d4ed8']
    gradeChart.setOption({
      tooltip: {},
      grid: { left: 40, right: 20, top: 20, bottom: 30 },
      xAxis: { type: 'category', data: rows.map(r => r.grade > 0 ? '♥'.repeat(r.grade) : '未设置') },
      yAxis: { type: 'value' },
      series: [{ type: 'bar', data: rows.map(r => ({ value: r.count, itemStyle: { color: r.grade > 0 ? (colors[r.grade-1] || '#3b82f6') : '#cbd5e1' } })) }],
    })
  }
  function onResize() { monthChart?.resize(); gradeChart?.resize() }
  onMount(() => { load(); window.addEventListener('resize', onResize) })
  onDestroy(() => { monthChart?.dispose(); gradeChart?.dispose(); window.removeEventListener('resize', onResize) })
</script>
<div class="space-y-4">
  <header><h1 class="text-2xl font-semibold">统计</h1><p class="text-sm mt-1" style="color: var(--q-muted);">周期性数据概览</p></header>
  <div class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3">月度活动</h2>
    <div bind:this={monthChartDiv} style="width: 100%; height: 240px;"></div>
  </div>
  <div class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3">亲密度分布</h2>
    <div bind:this={gradeChartDiv} style="width: 100%; height: 240px;"></div>
  </div>
</div>
