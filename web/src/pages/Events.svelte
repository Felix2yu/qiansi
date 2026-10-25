<script lang="ts">
  import { onMount } from 'svelte';
  import { API, TIMELINE_LABEL, yuan, todayLocal, type Event, type TimelineItem } from '../lib/api';
  import EventForm from '../lib/EventForm.svelte';
  import Modal from '../lib/ui/Modal.svelte';
  import PageHeader from '../lib/ui/PageHeader.svelte';
  import Empty from '../lib/ui/Empty.svelte';
  import LoadError from '../lib/ui/LoadError.svelte';
  import { route, navigate } from '../lib/router';
  import PersonPicker from '../lib/PersonPicker.svelte';
  import { selfFirst, personLabel, loadSelf } from '../lib/self.svelte';
  import { dict, ensure, refresh } from '../lib/dict.svelte';
  import { toast, errText } from '../lib/toast.svelte';
  import { ask } from '../lib/ask.svelte';
  import { eventDeleteAsk, trashOne } from '../lib/trash';
  import { Plus, Search, History, CalendarDays } from '@lucide/svelte';

  let { onlyTimeline = false }: { onlyTimeline?: boolean } = $props();
  let list = $state<Event[]>([]);
  let timeline = $state<TimelineItem[]>([]);
  // 加载失败与「没有记录」必须分开显示：前者是没读到，后者是真的没有
  let loadError = $state('');
  let timelineError = $state('');
  // 事件类型字典走共享缓存，不再每页重拉
  const types = $derived(dict.eventTypes);
  let showForm = $state(false);
  let editId = $state('');
  // 新建预填：引用保持稳定，组件只在打开时读取，变动会重置用户已填内容
  let formPreset = $state<{ event_date?: string; participant_ids?: string[] }>({});

  // 列表筛选：支持按参与人过滤（此前 person_id 被当成标题关键字，筛选形同虚设）
  let filterPerson = $state('');
  let filterQuery = $state('');
  let filterType = $state('');
  let filterFrom = $state('');
  let filterTo = $state('');
  const anyFilter = $derived(
    !!(filterPerson || filterQuery.trim() || filterType || filterFrom || filterTo)
  );

  function clearFilters() {
    filterPerson = '';
    filterQuery = '';
    filterType = '';
    filterFrom = '';
    filterTo = '';
    void load();
  }

  function openNew() {
    formPreset = { event_date: todayLocal() };
    editId = '';
    showForm = true;
  }
  function openEdit(id: string) {
    editId = id;
    showForm = true;
  }
  function closeForm() {
    showForm = false;
    editId = '';
  }
  async function afterForm() {
    closeForm();
    // 金钱页的「关联到哪场往来」用的就是这份缓存
    await Promise.all([load(), refresh('events')]);
  }

  const PAGE = 50;
  let page = $state(0);
  let hasMore = $state(false);

  // 时间线类型 → 统一语义色
  const TL_COLOR: Record<string, string> = {
    event: 'var(--q-c-event)',
    memo: 'var(--q-c-memo)',
    transaction: 'var(--q-c-money)'
  };

  // 时间线只在「全局时间线」视图里渲染，列表视图以前也照样拉一份
  async function loadTimeline() {
    try {
      timeline = await API.get<TimelineItem[]>('/api/v1/dashboard/timeline?limit=200');
      timelineError = '';
    } catch (err) {
      // 读失败也不能装作「暂无记录」：空列表和查不到是两件事
      timelineError = errText(err);
      toast.fail('加载失败', err);
    }
  }

  // 连点两个筛选项会并发发出两趟请求，谁后回来谁说了算，
  // 于是列表可能停在上一组条件的答案上。给每趟请求编号，只认最新那次。
  let reqId = 0;
  let loading = $state(false);

  async function load(reset = true) {
    if (reset) page = 0;
    const mine = ++reqId;
    loading = true;
    const qs = new URLSearchParams({ limit: String(PAGE), offset: String(page * PAGE) });
    if (filterPerson) qs.set('person_id', filterPerson);
    if (filterQuery.trim()) qs.set('q', filterQuery.trim());
    if (filterType) qs.set('type_id', filterType);
    if (filterFrom) qs.set('from', filterFrom);
    if (filterTo) qs.set('to', filterTo);
    let batch: Event[];
    try {
      batch = await API.get<Event[]>(`/api/v1/events?${qs}`);
    } catch (err) {
      if (mine === reqId) {
        loadError = errText(err);
        toast.fail('加载失败', err);
        loading = false;
      }
      return;
    }
    if (mine !== reqId) return;
    loading = false;
    loadError = '';
    hasMore = batch.length === PAGE;
    list = reset ? batch : [...list, ...batch];
  }
  async function loadMore() {
    const prev = page;
    page += 1;
    await load(false);
    // 失败时页码已自增，直接再点会跳过一整页且无提示；退回失败前的位置
    if (loadError) page = prev;
  }
  onMount(() => {
    loadSelf();
    if (onlyTimeline) {
      loadTimeline();
      return;
    }
    // 字典进缓存后，翻页与切筛选只剩 /api/v1/events 一个请求
    ensure('eventTypes');
    load(true);
  });

  // 首启引导用 /events?new=1 把「记一次往来」变成一次点击；渐远名单上的「记一笔」
  // 再带一个 with=<人物 id>，替用户把参与人填好。这里用 effect 而不是 onMount：
  // 这一页可能早就挂好了（切页不销毁组件），那时 onMount 永远不会再跑。
  $effect(() => {
    if (onlyTimeline) return;
    if ($route.query.new !== '1') return;
    const withId = $route.query.with;
    openNew();
    if (withId) formPreset = { ...formPreset, participant_ids: [withId] };
    navigate('/events');
  });

  // 删除只进回收站：5 秒内就地撤销，之后仍能从回收站请回来
  async function remove(id: string, title: string, expenseFen?: number) {
    if (!(await ask(eventDeleteAsk(title, expenseFen)))) return;
    if (!(await trashOne('event', id, () => Promise.all([load(), refresh('events')]), '已删除')))
      return;
    showForm = false;
    editId = '';
  }

  function locText(e: Event) {
    return e.locations && e.locations.length > 0 ? e.locations.join(' · ') : e.location || '';
  }
</script>

<div class="q-page">
  <PageHeader
    title={onlyTimeline ? '全局时间线' : '往来事件'}
    subtitle={onlyTimeline ? '所有类型混合视图' : '记录与亲友的见面、聚会、运动等'}
  >
    {#snippet actions()}
      {#if !onlyTimeline}
        <button class="q-btn q-btn--primary q-btn--sm" onclick={openNew}>
          <Plus size={14} /> 新建
        </button>
      {/if}
    {/snippet}
  </PageHeader>

  <div class="q-stack">
    {#if onlyTimeline}
      {#if timeline.length === 0}
        {#if timelineError}
          <LoadError text={`加载失败：${timelineError}`} onretry={() => loadTimeline()} />
        {:else}
          <Empty icon={History} text="还没有任何记录" />
        {/if}
      {:else}
        <div class="q-stack--tight flex flex-col">
          {#each timeline as t (t.id)}
            <div class="q-row items-start">
              <span class="q-dot mt-2" style="--c: {TL_COLOR[t.type] || 'var(--q-c-other)'};"
              ></span>
              <div class="min-w-0 flex-1">
                <div class="text-sm">{t.title}</div>
                <div class="q-num mt-0.5 text-xs" style="color: var(--q-muted);">
                  {TIMELINE_LABEL[t.type] || t.type}{t.person_name ? ' · ' + t.person_name : ''}
                  · {new Date(t.date).toLocaleDateString()}
                </div>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    {:else}
      <!-- 筛选工具条 -->
      <div class="q-card q-card--tight flex flex-wrap items-center gap-2">
        <div class="w-48">
          <PersonPicker
            bind:value={filterPerson}
            placeholder="全部参与人"
            compact={true}
            onchange={() => load()}
          />
        </div>
        <div class="relative min-w-[180px] flex-1">
          <Search
            size={14}
            class="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2"
            style="color: var(--q-faint);"
          />
          <input
            bind:value={filterQuery}
            onkeydown={(e) => e.key === 'Enter' && load()}
            placeholder="搜索标题 / 地点 / 备注"
            class="q-input !py-2 pl-9"
          />
        </div>
        <select
          bind:value={filterType}
          onchange={() => load()}
          aria-label="按类型筛选"
          class="q-select !py-2"
        >
          <option value="">全部类型</option>
          {#each types as t (t.id)}<option value={String(t.id)}>{t.name}</option>{/each}
        </select>
        <!-- 日期区间：手填的两个框，起止都给才限死，只给一个就是「从某天起 / 到某天止」 -->
        <div class="flex items-center gap-1.5">
          <input
            type="date"
            bind:value={filterFrom}
            onchange={() => load()}
            aria-label="起始日期"
            class="q-input !py-2"
          />
          <span class="text-xs" style="color: var(--q-muted);">至</span>
          <input
            type="date"
            bind:value={filterTo}
            onchange={() => load()}
            aria-label="截止日期"
            class="q-input !py-2"
          />
        </div>
        {#if anyFilter}
          <button class="q-btn q-btn--ghost q-btn--sm" onclick={clearFilters}>清除筛选</button>
        {/if}
      </div>

      <div class="q-stack--tight flex flex-col">
        {#each list as e (e.id)}
          <div class="q-card q-card--tight flex items-start gap-3">
            <span class="q-dot mt-2" style="--c: {e.type_color || 'var(--q-theme)'};"></span>
            <div class="min-w-0 flex-1">
              <div class="flex items-start justify-between gap-2">
                <div class="text-sm font-medium">{e.title}</div>
                <div class="flex shrink-0 items-center gap-1">
                  <button class="q-btn q-btn--ghost q-btn--sm" onclick={() => openEdit(e.id)}
                    >编辑</button
                  >
                  <button
                    class="q-btn q-btn--danger-ghost q-btn--sm"
                    onclick={() => remove(e.id, e.title, e.expense_fen)}>删除</button
                  >
                </div>
              </div>
              <div class="q-num mt-0.5 text-xs" style="color: var(--q-muted);">
                {#if e.type_name}<span>[{e.type_name}]</span>{/if}
                <span>{new Date(e.event_date).toLocaleDateString()}</span>
                {#if locText(e)}<span> · {locText(e)}</span>{/if}
                {#if e.participants && e.participants.length > 0}
                  <span> · </span>
                  {#each selfFirst(e.participants) as p, i (p.id)}
                    {#if i > 0}<span>, </span>{/if}
                    <a
                      class="link-quiet"
                      href={`/people/${p.id}`}
                      onclick={(ev) => {
                        ev.preventDefault();
                        navigate(`/people/${p.id}`);
                      }}>{personLabel(p)}</a
                    >
                  {/each}
                {/if}
              </div>
              <div class="mt-2 flex flex-wrap gap-1.5">
                {#if e.has_gift}<span class="q-badge">有礼物{e.gift ? `：${e.gift}` : ''}</span
                  >{/if}
                {#if e.expense_fen}
                  <span class="q-badge q-money">花费 ¥{yuan(e.expense_fen)}</span>
                {/if}
                {#if e.bill_count}
                  <span class="q-badge" title="开销由货殖账单汇总">货殖 {e.bill_count} 笔</span>
                {/if}
                {#if e.gift_amount_fen}
                  <span class="q-badge q-badge--theme q-money"
                    >{e.gift_direction === 'in' ? '收到礼金' : '随礼'}
                    ¥{yuan(e.gift_amount_fen)}</span
                  >
                {/if}
              </div>
              {#if e.summary}
                <div class="mt-1.5 line-clamp-2 text-sm" style="color: var(--q-muted);">
                  {e.summary}
                </div>
              {/if}
            </div>
          </div>
        {:else}
          {#if loadError}
            <LoadError text={`加载失败：${loadError}`} onretry={() => load()} />
          {:else if loading && list.length === 0}
            <div class="py-12 text-center text-sm" style="color: var(--q-muted);">加载中…</div>
          {:else}
            <Empty icon={CalendarDays} text="没有符合条件的往来" />
          {/if}
        {/each}
      </div>

      {#if loadError && list.length > 0}
        <div class="text-center text-sm" style="color: var(--q-danger);">
          加载失败：{loadError}
          <button class="q-btn q-btn--ghost q-btn--sm" onclick={() => load()}>重试</button>
        </div>
      {/if}

      {#if hasMore}
        <div class="text-center">
          <button class="q-btn q-btn--ghost" onclick={loadMore}>
            加载更多（已显示 {list.length} 条）
          </button>
        </div>
      {/if}
    {/if}
  </div>
</div>

{#if showForm && !onlyTimeline}
  <Modal wide onclose={closeForm} title={editId ? '编辑往来' : '新建往来'}>
    <EventForm
      {editId}
      preset={formPreset}
      {types}
      onsave={afterForm}
      oncancel={closeForm}
      onremove={remove}
    />
  </Modal>
{/if}
