<script lang="ts">
  import { onMount } from 'svelte'
  import { API, yuan, todayLocal, type Event, type EventType, type Person, type TimelineItem } from '../lib/api'
  import EventForm from '../lib/EventForm.svelte'
  import { Plus, X, Search } from '@lucide/svelte'

  let { onlyTimeline = false }: { onlyTimeline?: boolean } = $props()
  let list = $state<Event[]>([])
  let timeline = $state<TimelineItem[]>([])
  let types = $state<EventType[]>([])
  let people = $state<Person[]>([])
  let showForm = $state(false)
  let editId = $state('')
  // 新建预填：引用保持稳定，组件只在打开时读取，变动会重置用户已填内容
  let formPreset = $state<{ event_date?: string; participant_ids?: string[] }>({})

  // 列表筛选：支持按参与人过滤（此前 person_id 被当成标题关键字，筛选形同虚设）
  let filterPerson = $state('')
  let filterQuery = $state('')

  function openNew() {
    formPreset = { event_date: todayLocal() }
    editId = ''
    showForm = true
  }
  function openEdit(id: string) {
    editId = id
    showForm = true
  }
  function closeForm() {
    showForm = false
    editId = ''
  }
  async function afterForm() {
    closeForm()
    await load()
  }

  const PAGE = 50
  let page = $state(0)
  let hasMore = $state(false)

  async function load(reset = true) {
    if (reset) page = 0
    const qs = new URLSearchParams({ limit: String(PAGE), offset: String(page * PAGE) })
    if (filterPerson) qs.set('person_id', filterPerson)
    if (filterQuery.trim()) qs.set('q', filterQuery.trim())
    const [batch, ty, pe, tl] = await Promise.all([
      API.get(`/api/v1/events?${qs}`), API.get('/api/v1/event-types'),
      API.get('/api/v1/people?limit=500'), API.get('/api/v1/dashboard/timeline?limit=200'),
    ]) as any
    hasMore = (batch || []).length === PAGE
    list = reset ? batch : [...list, ...batch]
    types = ty; people = pe; timeline = tl
  }
  async function loadMore() {
    page += 1
    await load(false)
  }
  onMount(() => load(true))

  async function remove(id: string, title: string, expenseFen?: number) {
    const tail = expenseFen ? '该往来关联的开销账目会保留，仅解除关联。' : ''
    if (!confirm(`确定删除「${title}」吗？${tail}`)) return
    try {
      await API.delete(`/api/v1/events/${id}`)
    } catch (err: any) {
      alert('删除失败：' + (err?.message || err))
      return
    }
    showForm = false
    editId = ''
    await load()
  }

  function locText(e: Event) {
    return e.locations && e.locations.length > 0 ? e.locations.join(' · ') : (e.location || '')
  }
</script>
<div class="space-y-4">
  <header class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-semibold">{onlyTimeline ? '全局时间线' : '往来事件'}</h1>
      <p class="text-sm mt-1" style="color: var(--q-muted);">{onlyTimeline ? '所有类型混合视图' : '记录与亲友的见面、聚会、运动等'}</p>
    </div>
    {#if !onlyTimeline}
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={openNew}>
        <Plus size={14} /> 新建
      </button>
    {/if}
  </header>

  {#if onlyTimeline}
    <ul class="space-y-2">
      {#each timeline as t}
        <li class="rounded-lg p-3 flex items-start gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
          {#if t.type === 'event'}<div class="w-2 h-2 rounded-full mt-2" style="background: #10b981;"></div>
{:else if t.type === 'memo'}<div class="w-2 h-2 rounded-full mt-2" style="background: #f59e0b;"></div>
{:else if t.type === 'transaction'}<div class="w-2 h-2 rounded-full mt-2" style="background: #ef4444;"></div>
{:else}<div class="w-2 h-2 rounded-full mt-2" style="background: #6366f1;"></div>{/if}
          <div class="flex-1 min-w-0">
            <div class="text-sm">{t.title}</div>
            <div class="text-xs mt-0.5" style="color: var(--q-muted);">{t.type}{t.person_name ? ' · ' + t.person_name : ''} · {new Date(t.date).toLocaleDateString()}</div>
          </div>
        </li>
      {/each}
    </ul>
  {:else}
    <div class="flex flex-wrap gap-2">
      <select bind:value={filterPerson} onchange={() => load()} class="px-3 py-2 rounded-lg text-sm outline-none"
              style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);">
        <option value="">全部参与人</option>
        {#each people as p}<option value={p.id}>{p.name}</option>{/each}
      </select>
      <div class="relative flex-1 min-w-[180px]">
        <Search size={14} class="absolute left-3 top-1/2 -translate-y-1/2" style="color: var(--q-muted);" />
        <input bind:value={filterQuery} onkeydown={(e) => e.key === 'Enter' && load()} placeholder="搜索标题 / 地点 / 备注"
               class="w-full pl-9 pr-3 py-2 rounded-lg text-sm outline-none"
               style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);" />
      </div>
      {#if filterPerson || filterQuery}
        <button class="px-3 py-2 rounded-lg text-sm" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-muted);"
                onclick={() => { filterPerson = ''; filterQuery = ''; load() }}>清除筛选</button>
      {/if}
    </div>
    <ul class="space-y-2">
      {#each list as e}
        <li class="rounded-lg p-3 flex items-start gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
          <div class="w-2 h-2 rounded-full mt-2" style="background: {e.type_color || '#6366f1'};"></div>
          <div class="flex-1 min-w-0">
            <div class="flex items-start justify-between gap-2">
              <div class="text-sm font-medium">{e.title}</div>
              <div class="flex items-center gap-1 shrink-0">
                <button class="text-xs px-2 py-0.5 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-muted);" onclick={() => openEdit(e.id)}>编辑</button>
                <button class="text-xs px-2 py-0.5 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border); color: #ef4444;" onclick={() => remove(e.id, e.title, e.expense_fen)}>删除</button>
              </div>
            </div>
            <div class="text-xs mt-0.5" style="color: var(--q-muted);">
              {e.type_name && `[${e.type_name}] `}
              {new Date(e.event_date).toLocaleDateString()}
              {locText(e) && ` · ${locText(e)}`}
              {e.participants && e.participants.length > 0 && ` · ${e.participants.map(p => p.name).join(', ')}`}
            </div>
            <div class="flex flex-wrap gap-1.5 mt-1.5">
              {#if e.has_gift}
                <span class="text-xs px-1.5 py-0.5 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border);">有礼物{e.gift ? `：${e.gift}` : ''}</span>
              {/if}
              {#if e.expense_fen}
                <span class="text-xs px-1.5 py-0.5 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border);">花费 ¥{yuan(e.expense_fen)}</span>
              {/if}
            </div>
            {#if e.summary}<div class="text-sm mt-1 line-clamp-2" style="color: var(--q-muted);">{e.summary}</div>{/if}
          </div>
        </li>
      {:else}
        <li class="text-center py-8 text-sm" style="color: var(--q-muted);">没有符合条件的往来</li>
      {/each}
    </ul>
    {#if hasMore}
      <div class="text-center mt-3">
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-muted);" onclick={loadMore}>
          加载更多（已显示 {list.length} 条）
        </button>
      </div>
    {/if}
  {/if}
</div>

{#if showForm && !onlyTimeline}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4">
    <button type="button" aria-label="关闭弹窗" class="absolute inset-0 cursor-default" style="background: rgba(0,0,0,0.3); border: 0;" onclick={closeForm}></button>
    <div class="relative w-full max-w-lg max-h-[90vh] overflow-auto rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold">{editId ? '编辑往来' : '新建往来'}</h2>
        <button onclick={closeForm}><X size={18} /></button>
      </div>
      <EventForm {editId} preset={formPreset} {types} {people}
                 onsave={afterForm} oncancel={closeForm} onremove={remove} />
    </div>
  </div>
{/if}
