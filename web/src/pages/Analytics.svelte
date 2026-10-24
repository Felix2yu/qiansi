<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import * as echarts from 'echarts/core';
  import { BarChart } from 'echarts/charts';
  import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components';
  import { CanvasRenderer } from 'echarts/renderers';
  import { API, type GradeDist } from '../lib/api';
  import PageHeader from '../lib/ui/PageHeader.svelte';

  echarts.use([BarChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer]);

  let monthChartDiv: HTMLDivElement | null = null;
  let gradeChartDiv: HTMLDivElement | null = null;
  let monthChart: echarts.ECharts | null = null;
  let gradeChart: echarts.ECharts | null = null;

  // 读取设计令牌：图表的轴/提示文字跟随明暗主题，柱色仍是手定的纯色（不用渐变）
  function v(name: string, fallback: string) {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;
  }
  function chartChrome() {
    const muted = v('--q-muted', '#857b67');
    const border = v('--q-border', '#e1d8c6');
    const surface = v('--q-surface', '#fbf8f1');
    const text = v('--q-text', '#2b251b');
    return { muted, border, surface, text };
  }

  async function load() {
    const [month, grade] = (await Promise.all([
      API.get('/api/v1/dashboard/by-month'),
      API.get('/api/v1/dashboard/grade-distribution')
    ])) as any;
    drawMonth(month);
    drawGrade(grade);
  }
  function drawMonth(rows: any[]) {
    if (!monthChartDiv) return;
    const { muted, border, surface, text } = chartChrome();
    monthChart?.dispose();
    monthChart = echarts.init(monthChartDiv);
    monthChart.setOption({
      // 统一纸卡风提示框
      tooltip: {
        backgroundColor: surface,
        borderColor: border,
        textStyle: { color: text, fontSize: 12 },
        axisPointer: { type: 'shadow', shadowStyle: { color: 'rgba(120,100,60,0.06)' } }
      },
      // legend 必须显式放顶部并预留足够 grid.top，ECharts 默认会把它塞到底部挤 x 轴日期
      legend: {
        top: 4,
        left: 'center',
        itemWidth: 12,
        itemHeight: 10,
        icon: 'roundRect',
        textStyle: { fontSize: 12, color: muted }
      },
      grid: { left: 40, right: 20, top: 40, bottom: 32 },
      xAxis: {
        type: 'category',
        data: rows.map((r) => r.month),
        axisLine: { lineStyle: { color: border } },
        axisTick: { show: false },
        axisLabel: { color: muted, fontSize: 11 }
      },
      yAxis: {
        type: 'value',
        minInterval: 1,
        splitLine: { lineStyle: { color: border } },
        axisLabel: { color: muted, fontSize: 11 }
      },
      series: [
        {
          name: '往来',
          type: 'bar',
          data: rows.map((r) => r.event_count),
          barMaxWidth: 18,
          itemStyle: { color: v('--q-ok', '#3f7a54'), borderRadius: [3, 3, 0, 0] }
        },
        {
          name: '金钱',
          type: 'bar',
          data: rows.map((r) => r.tx_count),
          barMaxWidth: 18,
          itemStyle: { color: v('--q-c-money', '#bd3b2f'), borderRadius: [3, 3, 0, 0] }
        },
        {
          name: '对话',
          type: 'bar',
          data: rows.map((r) => r.memo_count),
          barMaxWidth: 18,
          itemStyle: { color: v('--q-warn', '#ad761d'), borderRadius: [3, 3, 0, 0] }
        }
      ]
    });
  }
  function drawGrade(rows: GradeDist[]) {
    if (!gradeChartDiv) return;
    const { muted, border, surface, text } = chartChrome();
    const dark = document.documentElement.classList.contains('dark');
    // 每档一根纯色柱：从沙白到沉绛，深浅分得清，不用渐变
    const colors = dark
      ? ['#6f675a', '#bfa077', '#c98a52', '#cf6a4d', '#d4533f']
      : ['#d8cfbd', '#c9a87a', '#b58354', '#a8513a', '#8c2f24'];
    gradeChart?.dispose();
    gradeChart = echarts.init(gradeChartDiv);
    gradeChart.setOption({
      tooltip: {
        backgroundColor: surface,
        borderColor: border,
        textStyle: { color: text, fontSize: 12 },
        axisPointer: { type: 'shadow', shadowStyle: { color: 'rgba(120,100,60,0.06)' } }
      },
      grid: { left: 40, right: 20, top: 20, bottom: 30 },
      xAxis: {
        type: 'category',
        data: rows.map((r) => (r.grade > 0 ? '♥'.repeat(r.grade) : '未设置')),
        axisLine: { lineStyle: { color: border } },
        axisTick: { show: false },
        axisLabel: { color: muted, fontSize: 11 }
      },
      yAxis: {
        type: 'value',
        minInterval: 1,
        splitLine: { lineStyle: { color: border } },
        axisLabel: { color: muted, fontSize: 11 }
      },
      series: [
        {
          type: 'bar',
          barMaxWidth: 34,
          data: rows.map((r) => ({
            value: r.count,
            itemStyle: {
              color: r.grade > 0 ? colors[r.grade - 1] || colors[4] : colors[0],
              borderRadius: [3, 3, 0, 0]
            }
          }))
        }
      ]
    });
  }
  function onResize() {
    monthChart?.resize();
    gradeChart?.resize();
  }
  onMount(() => {
    load();
    window.addEventListener('resize', onResize);
  });
  onDestroy(() => {
    monthChart?.dispose();
    gradeChart?.dispose();
    window.removeEventListener('resize', onResize);
  });
</script>

<div class="q-page">
  <PageHeader title="统计" subtitle="周期性数据概览" />
  <div class="q-stack">
    <div class="q-card">
      <h2 class="q-section-title">月度活动</h2>
      <div
        bind:this={monthChartDiv}
        class="q-chart-frame !border-0 !shadow-none"
        style="height: 240px; background: transparent;"
      ></div>
    </div>
    <div class="q-card">
      <h2 class="q-section-title">亲密度分布</h2>
      <div
        bind:this={gradeChartDiv}
        class="q-chart-frame !border-0 !shadow-none"
        style="height: 240px; background: transparent;"
      ></div>
    </div>
  </div>
</div>
