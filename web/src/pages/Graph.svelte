<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import * as echarts from 'echarts/core';
  import { GraphChart } from 'echarts/charts';
  import { TooltipComponent } from 'echarts/components';
  import { CanvasRenderer } from 'echarts/renderers';
  import {
    API,
    RELATION_TYPES,
    type Person,
    type Relationship,
    type Category,
    type Tag
  } from '../lib/api';
  import { navigate, route } from '../lib/router';
  import { self, loadSelf, personLabel } from '../lib/self.svelte';
  import { dict, ensure, refresh } from '../lib/dict.svelte';
  import {
    egoLayout,
    outwardLabel,
    circleFill,
    edgePairKey,
    laneOf,
    pairKey,
    introEdgePairs,
    introChain,
    vennLayout,
    type VennCircle
  } from '../lib/graphLayout';
  import { theme } from '../lib/theme.svelte';
  import { Plus, Link2, Route, Share2, Trash2 } from '@lucide/svelte';
  import PersonPicker from '../lib/PersonPicker.svelte';
  import Modal from '../lib/ui/Modal.svelte';
  import PageHeader from '../lib/ui/PageHeader.svelte';
  import Empty from '../lib/ui/Empty.svelte';
  import LoadError from '../lib/ui/LoadError.svelte';
  import { toast, errText } from '../lib/toast.svelte';

  echarts.use([GraphChart, TooltipComponent, CanvasRenderer]);

  type ViewMode = 'rel' | 'venn';

  let people = $state<Person[]>([]);
  // 加载失败与「还没有人物或关系」必须分开显示：前者是没读到，后者是真的没有
  let loadError = $state('');
  const categories = $derived(dict.categories);
  const allTags = $derived(dict.tags);
  let rels = $state<Relationship[]>([]);
  let tagsByPerson = $state<Record<string, Tag[]>>({});
  const usedTypes = $derived(dict.relTypes);
  let chartDiv: HTMLDivElement | null = $state(null);
  let chart: echarts.ECharts | null = null;
  // 容器尺寸变化（侧栏折叠、手机转屏、初始化时还没布局完）时让 ECharts 跟着重排，
  // 只监听 window resize 的话，零宽时刻 init 出来的图会一直是一条缝
  let ro: ResizeObserver | null = null;
  // 零尺寸首帧的「等尺寸」观察器：拿到宽度后立刻补画并断开
  let awaitRO: ResizeObserver | null = null;
  // mode=system 且系统切换外观时，theme.mode 本身不变，靠这个计数触发图表重绘
  let schemeTick = $state(0);
  const schemeMql =
    typeof matchMedia === 'function' ? matchMedia('(prefers-color-scheme: dark)') : null;
  // 圈层标尺只画连得上的人，剩下的人得说清楚去哪找
  let orphans = $state(0);
  let centered = $state(false);
  // 整图一次只取 500 个节点，超出部分后端会如实报截断，不能让人以为人丢了
  let graphMeta = $state({ truncated: false, total: 0, droppedEdges: 0 });

  let view = $state<ViewMode>('rel');
  // 关系视图筛选：空数组=不筛。选了只把不在其中的压暗而不是摘掉，节点一摘布局就跳。
  let catFilter = $state<number[]>([]);
  let tagFilter = $state<number[]>([]);
  // 认识路径开关：打开后引荐链画虚线，点节点从「进详情」变为「聚焦此人路径」
  let showChains = $state(false);
  let focusId = $state($route.query.focus || '');

  // 圈子视图：必选 2 个圈子，最多 3 个；第 4 个起在 UI 上禁选
  let vennSel = $state<number[]>([]);
  let vennEdges = $state(false);

  // 弹窗：rel=单条关系；chain=引荐链（我 —类型→ 人 —类型→ 人 …）
  let showForm = $state(false);
  let formMode = $state<'rel' | 'chain'>('rel');
  let pending = $state<{ from: string; to: string } | null>(null);
  let form = $state({ from_person_id: '', to_person_id: '', type: RELATION_TYPES[0], remark: '' });
  let chainRows = $state<{ type: string; personId: string; newName: string; isNew: boolean }[]>([
    { type: '同学', personId: '', newName: '', isNew: false }
  ]);
  let busy = $state(false);
  let dragEndedAt = 0;

  async function load() {
    // 圈子/标签/关系类型是全站共享字典，这里只保证「拿到」，不再各自重拉
    await Promise.all([ensure('categories'), ensure('tags'), ensure('relTypes')]);
    let r: any = null;
    try {
      r = await API.get<any>('/api/v1/relationships');
    } catch (err) {
      // 拉不到不能装作「还没有人物或关系」——空图和不画是两件事
      toast.fail('加载关系图失败', err);
      loadError = errText(err);
      return;
    }
    loadError = '';
    await loadSelf();
    people = r.people;
    rels = r.relationships;
    tagsByPerson = r.tags ?? {};
    graphMeta = {
      truncated: !!r.truncated,
      total: r.total_people ?? 0,
      droppedEdges: r.dropped_edges ?? 0
    };
    // 圈子可能刚被从设置里删掉，选择里留着死 id 会把维恩图选空
    vennSel = vennSel.filter((id) => categories.some((c) => c.id === id));
    catFilter = catFilter.filter((id) => categories.some((c) => c.id === id));
  }

  // chartDiv 要等 {#if} 走到有图的那一支才绑定上，load() 里同步画会拿到 null；
  // 交给 effect，数据或任一视图状态变化后重画。
  $effect(() => {
    // 把影响绘图的状态都读一遍，任何一个变了就重绘
    const ready = people.length > 0;
    const canDraw = ready && (view === 'rel' || (view === 'venn' && vennSel.length >= 2));
    // 下面这些状态虽不参与分支判断，也得显式读取以建立依赖
    void catFilter.length;
    void tagFilter.length;
    void showChains;
    void focusId;
    void vennSel.length;
    void vennEdges;
    // 明/暗主题与自定义主题色变化时，画布上的墨色/朱色也要跟着换
    void theme.mode;
    void theme.color;
    void schemeTick;

    awaitRO?.disconnect();
    const el = chartDiv;
    if (!el || !canDraw) return;
    // ECharts 在 0×0 容器上会算出不可逆（奇异）矩阵，view 的 legacyCopyOverallTrans
    // 随即抛 “Cannot read properties of null (reading '0')”。首帧零宽时先等尺寸。
    if (el.clientWidth > 0 && el.clientHeight > 0) {
      renderChart();
      return;
    }
    awaitRO = new ResizeObserver(() => {
      if (el.clientWidth > 0 && el.clientHeight > 0) {
        awaitRO?.disconnect();
        awaitRO = null;
        if (chartDiv === el) renderChart();
      }
    });
    awaitRO.observe(el);
    return () => awaitRO?.disconnect();
  });

  $effect(() => {
    // 浏览器前进后退 / 详情页深链过来时同步聚焦人
    focusId = $route.query.focus || '';
    if (focusId) view = 'rel';
  });

  const typeOptions = $derived(
    Array.from(
      new Set([...RELATION_TYPES, ...usedTypes, ...rels.map((r) => r.type).filter(Boolean)])
    )
  );

  // 维恩布局算一次：绘图和「圈外人物」chips 共用，别在模板里每渲染一次就松弛 240 轮
  const vennCircles = $derived<VennCircle[]>(
    vennSel
      .map((id) => categories.find((c) => c.id === id))
      .filter((c): c is Category => !!c)
      .map((c) => ({ id: c.id, name: c.name, color: c.color }))
  );
  const venn = $derived(
    view === 'venn' && vennCircles.length >= 2 ? vennLayout(vennCircles, people) : null
  );

  const dimByFilter = (p: Person) => {
    if (catFilter.length > 0 && !(p.categories ?? []).some((c) => catFilter.includes(c.id)))
      return true;
    if (tagFilter.length > 0 && !(tagsByPerson[p.id] ?? []).some((t) => tagFilter.includes(t.id)))
      return true;
    return false;
  };

  function hexA(hex: string, alpha: number): string {
    const m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(hex.trim());
    if (!m) return hex;
    let h = m[1];
    if (h.length === 3)
      h = h
        .split('')
        .map((ch) => ch + ch)
        .join('');
    const n = parseInt(h, 16);
    return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${alpha})`;
  }

  // 画布上不能用 CSS 变量（canvas fillStyle 不支持），每次重绘前从计算样式取实色
  function chartPalette() {
    const css = (n: string) =>
      getComputedStyle(document.documentElement).getPropertyValue(n).trim();
    return {
      ink: css('--q-text'),
      muted: css('--q-muted'),
      faint: css('--q-faint'),
      surface: css('--q-surface'),
      // 本人 / 引荐链的强调色沿用赭黄语义（--q-warn），与朱砂主色拉开
      accent: css('--q-warn')
    };
  }

  // 节点填色：有圈子按圈子硬边拼色；一个圈子都没有时用当前主题的暖灰实色，
  // 不能把空数组交给 circleFill（它内部的兜底色不随明暗主题变化）
  function nodeFill(p: Person, fallback: string) {
    const colors = (p.categories ?? []).map((c) => c.color).filter(Boolean) as string[];
    return circleFill(colors.length ? colors : [fallback]);
  }

  // 关系视图与圈子视图是两个不同的容器 div（同一时刻只渲染一个），
  // 切换时旧实例必须随旧容器释放；同视图内的筛选/主题变化则复用实例，
  // 用 notMerge 整幅重画——避免 dispose 竞态，也跳过通用过渡的坐标系拷贝。
  function ensureChart(el: HTMLDivElement): echarts.ECharts {
    if (chart && chart.getDom() !== el) {
      chart.dispose();
      ro?.disconnect();
      chart = null;
    }
    if (!chart) {
      chart = echarts.init(el);
      chart.on('click', onChartClick);
      ro = new ResizeObserver(() => {
        // 抽屉展开/转屏瞬间可能给 0 尺寸，resize 到 0 同样触发奇异矩阵
        if (el.clientWidth > 0 && el.clientHeight > 0) chart?.resize();
      });
      ro.observe(el);
    }
    return chart;
  }

  function renderChart() {
    const el = chartDiv;
    if (!el || el.clientWidth === 0 || el.clientHeight === 0) return;
    const option = view === 'venn' ? buildVennOption() : buildRelOption();
    if (!option) return;
    ensureChart(el).setOption(option, { notMerge: true });
  }

  function onChartClick(p: any) {
    if (Date.now() - dragEndedAt < 300) return;
    // 点连线 = 给这对人补一种关系。以前这里只认 node，边上是死区，
    // 而表单里那套「来自图上连线」的预填界面（pending）一直没人喂过数据。
    if (p?.dataType === 'edge' && view !== 'venn') {
      const from = p.data?.source as string | undefined;
      const to = p.data?.target as string | undefined;
      if (from && to && from !== to) openForm('rel', from, to);
      return;
    }
    if (p?.dataType !== 'node' || !p?.data?.id || p.data.guide || p.data.vennBg) return;
    const id = p.data.id as string;
    if (view === 'venn') {
      navigate(`/people/${id}`);
      return;
    }
    // 认识路径模式下点节点 = 聚焦此人；否则进详情维护关系
    if (showChains) navigate(`/graph?focus=${encodeURIComponent(id)}`);
    else navigate(`/people/${id}`);
  }

  function buildRelOption() {
    const pal = chartPalette();
    // 端点被归档的人不在 nodes 里，那条边会指向不存在的节点，得先滤掉
    const ids = new Set(people.map((p) => p.id));
    const edges = rels.filter((r) => ids.has(r.from_person_id) && ids.has(r.to_person_id));
    // 本人用赭黄而不是朱砂主色：跟其他节点同色就谈不上高亮了
    const accent = pal.accent;
    // 无圈子的人用暖灰实色填充（旧版冷灰 #94a3b8 在纸面上发脏）
    const neutral = pal.muted;
    const gradeOf = new Map(people.map((p) => [p.id, p.grade]));
    // 没设本人就无所谓「离我几步」；本人被归档时也一样，退回原来的力导向
    const layout =
      self.id && ids.has(self.id) ? egoLayout(self.id, edges, (id) => gradeOf.get(id) ?? 0) : null;
    const shown = layout ? people.filter((p) => layout.pos.has(p.id)) : people;
    orphans = layout ? people.length - shown.length : 0;
    centered = !!layout;

    // 认识路径：聚焦时只留链上节点与边；开关打开但未聚焦时，所有引荐边虚线衬出
    const allIntroPairs = introEdgePairs(people);
    let chainIds: Set<string> | null = null;
    let chainPairs: Set<string> | null = null;
    if (focusId) {
      const chain = self.id ? introChain(self.id, focusId, people, rels) : null;
      if (chain && (chain.reachesSelf || chain.chainIds.length > 1)) {
        chainIds = new Set(chain.chainIds);
        chainPairs = new Set();
        for (let i = 0; i < chain.chainIds.length - 1; i++) {
          chainPairs.add(pairKey(chain.chainIds[i], chain.chainIds[i + 1]));
        }
      }
    }
    const focusPair = focusId && self.id ? pairKey(self.id, focusId) : '';

    const dimOf = (p: Person) => {
      if (dimByFilter(p)) return true;
      if (chainIds && !chainIds.has(p.id)) return true;
      return false;
    };

    const nodes: any[] = shown.map((p) => {
      const me = p.id === self.id;
      const at = layout?.pos.get(p.id);
      const dim = dimOf(p);
      const onChain = !!chainIds?.has(p.id);
      return {
        id: p.id,
        name: me ? `我 · ${p.name}` : p.name,
        ...(at && { x: at.x, y: at.y }),
        symbolSize: me ? 46 : 18 + p.grade * 4,
        label: {
          position: at ? (me ? 'bottom' : outwardLabel(at.angle)) : 'bottom',
          color: pal.ink,
          ...(me && { fontWeight: 'bold' }),
          ...(dim && { opacity: 0.15 }),
          ...(onChain && !me && { color: accent, fontWeight: 600 })
        },
        itemStyle: me
          ? { color: accent, borderColor: pal.surface, borderWidth: 2 }
          : {
              color: nodeFill(p, neutral),
              ...(onChain && { borderColor: accent, borderWidth: 2 }),
              ...(dim && { opacity: 0.12 })
            }
      };
    });
    if (layout) {
      // 圈层标尺：symbol 用 'none' 时 echarts 连标签一起吞了，所以留个小点
      for (const [k, r] of layout.radii) {
        nodes.push({
          id: `hop-${k}`,
          name: `${k} 步`,
          x: 0,
          y: -r,
          symbolSize: 5,
          draggable: false,
          guide: true,
          itemStyle: { color: hexA(pal.muted, 0.35) },
          label: { show: true, position: 'right', fontSize: 11, color: pal.muted }
        });
      }
    }
    const dimIds = new Set(shown.filter(dimOf).map((p) => p.id));
    // 一对人现在可以并存几条不同类型的边，echarts 会把它们画成同一条线，标签叠死。
    // 按无序人对分组，同组的边轮流分到弯曲车道上；只有一条时留空，用系列默认的 0.1。
    const pairCount = new Map<string, number>();
    for (const r of edges) {
      const k = edgePairKey(r);
      pairCount.set(k, (pairCount.get(k) ?? 0) + 1);
    }
    const pairUsed = new Map<string, number>();
    const links = edges.map((r) => {
      const pk = edgePairKey(r);
      const mine = !!self.id && (r.from_person_id === self.id || r.to_person_id === self.id);
      const onChain = chainPairs?.has(pk);
      const isDirectFocus = !!focusPair && pk === focusPair;
      const dim = dimIds.has(r.from_person_id) || dimIds.has(r.to_person_id);
      const i = pairCount.get(pk)! > 1 ? (pairUsed.get(pk) ?? 0) : -1;
      pairUsed.set(pk, i + 1);
      const curve = laneOf(i, r.from_person_id <= r.to_person_id);
      // 链边赭黄虚线；我与聚焦人的直达边赭黄实线（可能与链边重合，重合时按链边画）
      const chainEdge = showChains && allIntroPairs.has(pk);
      let color = mine ? accent : pal.faint;
      let opacity = mine ? 1 : 0.55;
      let type: 'solid' | 'dashed' = 'solid';
      let width = mine ? 2.5 : 1;
      if (onChain) {
        color = accent;
        opacity = 1;
        type = 'dashed';
        width = 2.5;
      } else if (isDirectFocus) {
        color = accent;
        opacity = 1;
        width = 2.5;
      } else if (!focusId && chainEdge) {
        color = accent;
        opacity = 0.8;
        type = 'dashed';
        width = 1.6;
      }
      if (dim) opacity = 0.04;
      return {
        source: r.from_person_id,
        target: r.to_person_id,
        lineStyle: {
          ...(curve !== undefined && { curveness: curve }),
          color,
          width,
          opacity,
          type
        },
        label: {
          show: !dim,
          formatter: r.type,
          fontSize: 10,
          color: onChain || isDirectFocus || mine ? accent : pal.muted
        }
      };
    });
    return {
      tooltip: { show: false },
      series: [
        {
          type: 'graph',
          layout: layout ? 'none' : 'force',
          roam: true,
          draggable: true,
          ...(layout
            ? { left: 'center', top: 'middle' }
            : {
                force: { repulsion: 250, edgeLength: 120 },
                left: '8%',
                right: '8%',
                top: '10%',
                bottom: '8%'
              }),
          emphasis: { focus: 'adjacency', lineStyle: { width: 3 } },
          lineStyle: { color: 'source', curveness: 0.1, opacity: 0.7 },
          label: { show: true, fontSize: 12, position: 'bottom', color: pal.ink },
          edgeLabel: { position: 'middle' },
          data: nodes,
          links
        }
      ]
    };
  }

  function buildVennOption() {
    const pal = chartPalette();
    const selCircles = vennCircles;
    const layout = venn;
    if (!layout || selCircles.length < 2) return null;
    const placed = new Set(layout.pos.keys());

    // 背景大圆必须排在人物节点前面：同一系列按 data 顺序绘制，先画的被压在下面
    const nodes: any[] = layout.circles.map((z, i) => ({
      id: `venn-circle-${selCircles[i].id}`,
      name: selCircles[i].name,
      x: z.cx,
      y: z.cy,
      symbolSize: z.r * 2,
      symbol: 'circle',
      draggable: false,
      vennBg: true,
      itemStyle: {
        color: hexA(selCircles[i].color, 0.09),
        borderColor: hexA(selCircles[i].color, 0.75),
        borderWidth: 2
      },
      label: {
        show: true,
        position: [0, -z.r + 26],
        color: selCircles[i].color,
        fontSize: 15,
        fontWeight: 700
      }
    }));

    for (const p of people) {
      const at = layout.pos.get(p.id);
      if (!at) continue;
      const me = p.id === self.id;
      const dim = dimByFilter(p);
      nodes.push({
        id: p.id,
        name: me ? `我 · ${p.name}` : p.name,
        x: at.x,
        y: at.y,
        symbolSize: me ? 46 : 18 + p.grade * 4,
        label: {
          show: true,
          position: 'bottom',
          fontSize: 12,
          color: pal.ink,
          ...(me && { fontWeight: 'bold' }),
          ...(dim && { opacity: 0.2 })
        },
        itemStyle: me
          ? { color: pal.accent, borderColor: pal.surface, borderWidth: 2 }
          : {
              color: nodeFill(p, pal.muted),
              ...(dim && { opacity: 0.15 })
            }
      });
    }

    let links: any[] = [];
    if (vennEdges) {
      links = rels
        .filter((r) => placed.has(r.from_person_id) && placed.has(r.to_person_id))
        .map((r) => ({
          source: r.from_person_id,
          target: r.to_person_id,
          lineStyle: { curveness: 0, opacity: 0.35, color: pal.faint },
          label: { show: false }
        }));
    }

    orphans = 0;
    centered = false;
    return {
      tooltip: { show: false },
      series: [
        {
          type: 'graph',
          layout: 'none',
          roam: true,
          draggable: false,
          left: 'center',
          top: 'middle',
          emphasis: { focus: 'adjacency' },
          label: { show: false, color: pal.ink },
          data: nodes,
          links
        }
      ]
    };
  }

  function toggleCat(id: number) {
    catFilter = catFilter.includes(id) ? catFilter.filter((x) => x !== id) : [...catFilter, id];
  }
  function toggleTag(id: number) {
    tagFilter = tagFilter.includes(id) ? tagFilter.filter((x) => x !== id) : [...tagFilter, id];
  }
  function toggleVenn(id: number) {
    if (vennSel.includes(id)) vennSel = vennSel.filter((x) => x !== id);
    else if (vennSel.length < 3) vennSel = [...vennSel, id];
  }

  function openForm(mode: 'rel' | 'chain' = 'rel', from = '', to = '') {
    formMode = mode;
    if (!from && !to) from = self.id;
    form = { from_person_id: from, to_person_id: to, type: RELATION_TYPES[0], remark: '' };
    pending = from && to ? { from, to } : null;
    if (mode === 'chain') {
      chainRows = [{ type: '同学', personId: '', newName: '', isNew: false }];
    }
    showForm = true;
  }
  function closeForm() {
    showForm = false;
    pending = null;
  }

  async function submitRel() {
    if (!form.from_person_id || !form.to_person_id) {
      toast.error('请选择关系的双方');
      return;
    }
    if (form.from_person_id === form.to_person_id) {
      toast.error('不能与自己建立关系');
      return;
    }
    if (!form.type.trim()) {
      toast.error('请填写关系类型');
      return;
    }
    busy = true;
    try {
      await API.post('/api/v1/relationships', { ...form, type: form.type.trim() });
    } catch (err) {
      // 失败时弹窗留着、双方已选的人不丢，改完可以直接再存一次
      toast.fail('添加失败', err);
      return;
    } finally {
      busy = false;
    }
    closeForm();
    toast.ok('已添加关系');
    // 关系类型可能是现场敲的新词，字典得知道
    await Promise.all([load(), refresh('relTypes')]);
  }

  function addChainRow() {
    chainRows = [...chainRows, { type: '', personId: '', newName: '', isNew: false }];
  }
  function removeChainRow(i: number) {
    chainRows = chainRows.filter((_, idx) => idx !== i);
  }
  // 每一跳要么选一个已有的人，要么当场新建；两种输入不能同时留着
  function toggleChainNew(i: number) {
    chainRows = chainRows.map((r, idx) =>
      idx !== i
        ? r
        : {
            ...r,
            isNew: !r.isNew,
            personId: r.isNew ? r.personId : '',
            newName: r.isNew ? '' : r.newName
          }
    );
  }

  // 引荐链提交：缺人的先建（仅姓名，引荐人=上一跳），再逐跳建边。
  // 已有人物不覆盖其引荐人；边已存在（409）视为跳过。中途失败不回滚，提示断点。
  async function submitChain() {
    if (!self.id) {
      toast.error('请先在人物详情里把自己设为「本人」');
      return;
    }
    const rows = chainRows.filter(
      (r) => r.type.trim() && (r.isNew ? r.newName.trim() : r.personId)
    );
    if (rows.length === 0) {
      toast.error('至少填写一跳：关系类型 + 人物');
      return;
    }
    busy = true;
    const failures: string[] = [];
    let prevId = self.id;
    try {
      for (let i = 0; i < rows.length; i++) {
        const row = rows[i];
        let pid = row.personId;
        const isNew = row.isNew;
        if (isNew) {
          // 第一跳与我直达，不设引荐人；其后每人的引荐人都是上一跳
          const body: any = { name: row.newName.trim() };
          if (prevId !== self.id) body.introduced_by_person_id = prevId;
          const created = await API.post<Person>('/api/v1/people', body);
          pid = created.id;
        } else if (prevId !== self.id) {
          // 已有人物：仅在还没记录引荐人时补上，不覆盖。
          // 图上只画了前 500 人，搜到的人可能不在这一页里，取不到就按 id 回查。
          const existing =
            people.find((p) => p.id === pid) ??
            (await API.get<{ person: Person }>(`/api/v1/people/${pid}`)
              .then((r) => r.person)
              .catch((e: any) => {
                // 回查失败就补不了引荐人，得说出来，不然链看着像全存上了
                failures.push(`第 ${i + 1} 跳查联系人档案失败，引荐人未补记：${errText(e)}`);
                return undefined;
              }));
          if (existing && !existing.introduced_by_person_id) {
            const full: any = {
              ...existing,
              category_ids: (existing.categories ?? []).map((c) => c.id),
              introduced_by_person_id: prevId
            };
            await API.put(`/api/v1/people/${pid}`, full).catch((e: any) =>
              failures.push(`补「${existing.name}」引荐人失败：${errText(e)}`)
            );
          }
        }
        try {
          await API.post('/api/v1/relationships', {
            from_person_id: prevId,
            to_person_id: pid,
            type: row.type.trim(),
            remark: ''
          });
        } catch (e) {
          const msg = errText(e);
          if (!msg.includes('已经有同名')) failures.push(`第 ${i + 1} 跳关系创建失败：${msg}`);
        }
        prevId = pid;
      }
      closeForm();
      // 链上可能敲了新关系类型，字典要跟上
      await Promise.all([load(), refresh('relTypes')]);
      if (failures.length) toast.error('部分环节未成功：' + failures.join('；'));
      else toast.ok('已录入引荐链');
    } catch (err) {
      // 建人到一半失败会直接抛出循环：弹窗不关、已填的跳不丢，同时说清断在哪
      toast.fail('引荐链保存中断', err);
    } finally {
      busy = false;
    }
  }

  const outsideChips = $derived(venn ? people.filter((p) => !venn.pos.has(p.id)) : []);

  onMount(() => {
    load();
    schemeMql?.addEventListener('change', onSchemeChange);
  });
  onDestroy(() => {
    chart?.dispose();
    ro?.disconnect();
    awaitRO?.disconnect();
    schemeMql?.removeEventListener('change', onSchemeChange);
  });

  function onSchemeChange() {
    schemeTick += 1;
  }
</script>

<div class="q-page">
  <PageHeader
    title="关系图"
    subtitle={view === 'rel'
      ? centered
        ? '圈层 = 关系上离我几步，同圈按亲密度排 · 虚线是「通过谁认识」的引荐链'
        : '在人物详情里设为本人后，图会以你为中心按亲密度铺开'
      : '圈子是集合：同时属于两个圈子的人落在交集里 · 选 2-3 个圈子对比'}
  >
    {#snippet actions()}
      <div class="q-seg">
        <button class="q-seg__btn" aria-pressed={view === 'rel'} onclick={() => (view = 'rel')}>
          <Share2 size={14} /> 关系
        </button>
        <button class="q-seg__btn" aria-pressed={view === 'venn'} onclick={() => (view = 'venn')}>
          <Link2 size={14} /> 圈子
        </button>
      </div>
      <button class="q-btn q-btn--primary q-btn--sm" onclick={() => openForm('rel')}>
        <Plus size={14} /> 添加关系
      </button>
    {/snippet}
  </PageHeader>

  <div class="q-stack">
    {#if people.length === 0}
      {#if loadError}
        <LoadError text={`加载失败：${loadError}`} onretry={() => load()} />
      {:else}
        <Empty icon={Share2} text="还没有人物或关系，先去通讯录记一个人" />
      {/if}
    {:else}
      {#if view === 'rel'}
        <div class="q-card q-card--tight flex flex-wrap items-center gap-2">
          {#if categories.length > 0}
            <span class="q-hint !mt-0 mr-1">按圈子筛</span>
            {#each categories as c (c.id)}
              <button
                class="q-chip q-chip-cat"
                style="--c: {c.color};"
                aria-pressed={catFilter.includes(c.id)}
                onclick={() => toggleCat(c.id)}>{c.name}</button
              >
            {/each}
          {/if}
          {#if allTags.length > 0}
            <span class="q-hint !mt-0 mr-1 ml-2">标签</span>
            {#each allTags as t (t.id)}
              <button
                class="q-chip q-chip-cat"
                style="--c: {t.color};"
                aria-pressed={tagFilter.includes(t.id)}
                onclick={() => toggleTag(t.id)}>#{t.name}</button
              >
            {/each}
          {/if}
          {#if catFilter.length > 0 || tagFilter.length > 0}
            <button
              class="q-chip"
              onclick={() => {
                catFilter = [];
                tagFilter = [];
              }}>清除</button
            >
          {/if}
          <label
            class="ml-auto flex cursor-pointer items-center gap-1.5 text-xs select-none"
            style="color: var(--q-muted);"
          >
            <Route size={14} />
            <input type="checkbox" bind:checked={showChains} />
            认识路径（开启后点节点聚焦）
          </label>
          {#if focusId}
            {@const fp = people.find((p) => p.id === focusId)}
            <button
              class="q-chip"
              aria-pressed="true"
              style="background: color-mix(in srgb, var(--q-warn) 16%, var(--q-surface)); border-color: color-mix(in srgb, var(--q-warn) 55%, var(--q-border)); color: var(--q-warn);"
              onclick={() => navigate('/graph')}
            >
              聚焦：{fp ? personLabel(fp) : '?'} ✕
            </button>
          {/if}
        </div>

        <!-- 图表容器：640px 高度沿用既有几何，画布配色随设计令牌走 -->
        <div bind:this={chartDiv} class="q-chart-frame" style="height: 640px;"></div>
        {#if graphMeta.truncated}
          <button
            class="q-btn q-btn--subtle q-btn--sm q-btn--block"
            onclick={() => navigate('/people')}
          >
            通讯录共 {graphMeta.total} 人，图里只画前 {people.length} 人（按等级、最近更新排）
            {#if graphMeta.droppedEdges > 0}，另有 {graphMeta.droppedEdges} 条关系因端点未进图而暂未显示{/if}
            · 去通讯录
          </button>
        {/if}
        {#if centered && orphans > 0}
          <button
            class="q-btn q-btn--subtle q-btn--sm q-btn--block"
            onclick={() => navigate('/people')}
          >
            另有 {orphans} 人还没有关系连线，未进图 · 去通讯录
          </button>
        {/if}
      {:else}
        {#if categories.length < 2}
          <Empty icon={Link2} text="至少需要两个圈子才能画交叠圈">
            {#snippet actions()}
              <button class="q-btn q-btn--primary q-btn--sm" onclick={() => navigate('/settings')}>
                去设置里建圈子
              </button>
            {/snippet}
          </Empty>
        {:else}
          <div class="q-card q-card--tight flex flex-wrap items-center gap-2">
            <span class="q-hint !mt-0 mr-1">选圈子（2-3 个）</span>
            {#each categories as c (c.id)}
              <button
                class="q-chip q-chip-cat disabled:opacity-40"
                style="--c: {c.color};"
                disabled={!vennSel.includes(c.id) && vennSel.length >= 3}
                aria-pressed={vennSel.includes(c.id)}
                onclick={() => toggleVenn(c.id)}>{c.name}</button
              >
            {/each}
            <label
              class="ml-auto flex cursor-pointer items-center gap-1.5 text-xs select-none"
              style="color: var(--q-muted);"
            >
              <input type="checkbox" bind:checked={vennEdges} />
              叠加关系连线
            </label>
          </div>

          {#if vennSel.length < 2}
            <Empty icon={Link2} text="请选择两个圈子查看交集" />
          {:else}
            <div bind:this={chartDiv} class="q-chart-frame" style="height: 680px;"></div>
            {#if outsideChips.length > 0}
              <div class="q-card q-card--tight flex flex-wrap items-center gap-1.5">
                <span class="q-hint !mt-0 mr-1">不在所选圈子（{outsideChips.length}）</span>
                {#each outsideChips as p (p.id)}
                  <button class="q-chip" onclick={() => navigate(`/people/${p.id}`)}
                    >{personLabel(p)}</button
                  >
                {/each}
              </div>
            {/if}
          {/if}
        {/if}
      {/if}
    {/if}
  </div>
</div>

{#if showForm}
  <Modal wide onclose={closeForm} title={formMode === 'chain' ? '录入引荐链' : '添加关系'}>
    <div class="q-seg mb-4">
      <button
        class="q-seg__btn"
        aria-pressed={formMode === 'rel'}
        onclick={() => (formMode = 'rel')}>单条关系</button
      >
      <button
        class="q-seg__btn"
        aria-pressed={formMode === 'chain'}
        onclick={() => openForm('chain')}
      >
        <Route size={13} /> 引荐链
      </button>
    </div>

    <!-- 关系类型候选词，单条/引荐链共用，全弹窗只保留一份 -->
    <datalist id="rel-type-options">
      {#each typeOptions as t (t)}<option value={t}></option>{/each}
    </datalist>

    {#if formMode === 'rel'}
      {#if !self.id}
        <p class="q-hint mb-3">还没设「本人」，下面可以任选两人连线。</p>
      {/if}
      {#if pending}
        <div class="q-well mb-3 flex items-center gap-2 !py-2.5 text-sm">
          <span class="font-medium">{people.find((p) => p.id === pending!.from)?.name || '?'}</span>
          <span class="q-hint !mt-0">→</span>
          <span class="font-medium">{people.find((p) => p.id === pending!.to)?.name || '?'}</span>
          <span class="q-hint !mt-0 ml-auto">来自图上连线</span>
        </div>
      {/if}
      <div class="q-stack--tight flex flex-col">
        {#if pending}
          <p class="q-hint !mt-0">这一对来自图上的连线，直接填关系类型即可。</p>
        {:else}
          <div class="grid grid-cols-2 gap-2">
            <PersonPicker
              bind:value={form.from_person_id}
              exclude={[form.to_person_id]}
              placeholder="甲方…"
              clearable={false}
            />
            <PersonPicker
              bind:value={form.to_person_id}
              exclude={[form.from_person_id]}
              placeholder="乙方…"
              clearable={false}
            />
          </div>
        {/if}
        <input
          list="rel-type-options"
          bind:value={form.type}
          placeholder="关系类型（可自定义：闺蜜、对象、挚友…）"
          class="q-input"
        />
        <input bind:value={form.remark} placeholder="备注（可选）" class="q-input" />
      </div>
    {:else}
      {#if !self.id}
        <p class="q-badge q-badge--danger mb-3">
          请先在人物详情里把自己设为「本人」，再录入引荐链。
        </p>
      {/if}
      <p class="q-hint mb-3">
        录「我 →同学→ A →对象→ B →闺蜜→ C」这样的认识经过。没有的人可以当场新建（只填名字），
        系统会给新人物自动记好引荐人；之后再补一条「我 —挚友→ C」的直达关系即可。
      </p>
      <div class="q-stack--tight mb-2 flex flex-col">
        {#each chainRows as row, i (i)}
          <div class="flex items-center gap-2">
            <span class="q-hint !mt-0 w-14 shrink-0 text-right">{i === 0 ? '我' : '再经'}</span>
            <input
              list="rel-type-options"
              bind:value={row.type}
              placeholder="关系，如 同学/对象/闺蜜"
              class="q-input w-32 shrink-0"
            />
            <span class="q-hint !mt-0">→</span>
            {#if row.isNew}
              <input
                bind:value={row.newName}
                placeholder="新人姓名"
                class="q-input min-w-0 flex-1"
              />
            {:else}
              <div class="min-w-0 flex-1">
                <PersonPicker
                  bind:value={row.personId}
                  placeholder="选择已有联系人…"
                  clearable={false}
                />
              </div>
            {/if}
            <button
              class="q-btn q-btn--ghost q-btn--sm shrink-0"
              title={row.isNew ? '改为选择已有联系人' : '不选人，当场新建一个'}
              onclick={() => toggleChainNew(i)}
            >
              {row.isNew ? '改选人' : '＋新建'}
            </button>
            {#if chainRows.length > 1}
              <button class="q-iconbtn !h-8 !w-8 shrink-0" onclick={() => removeChainRow(i)}
                ><Trash2 size={14} /></button
              >
            {/if}
          </div>
        {/each}
        <button class="q-btn q-btn--ghost q-btn--sm w-fit" onclick={addChainRow}>
          <Plus size={12} /> 加一跳
        </button>
      </div>
    {/if}

    {#snippet foot()}
      <button class="q-btn q-btn--ghost" onclick={closeForm}>取消</button>
      {#if formMode === 'rel'}
        <button class="q-btn q-btn--primary" disabled={busy} onclick={submitRel}>保存</button>
      {:else}
        <button class="q-btn q-btn--primary" disabled={busy || !self.id} onclick={submitChain}>
          保存整条链
        </button>
      {/if}
    {/snippet}
  </Modal>
{/if}
