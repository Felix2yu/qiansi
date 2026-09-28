<script lang="ts">
  import { onMount } from 'svelte'
  import { API, yuan, type Event, type EventType, type Person, type TimelineItem } from '../lib/api'
  import { Plus, X } from '@lucide/svelte'

  let { onlyTimeline = false }: { onlyTimeline?: boolean } = $props()
  let list = $state<Event[]>([])
  let timeline = $state<TimelineItem[]>([])
  let types = $state<EventType[]>([])
  let people = $state<Person[]>([])
  let showForm = $state(false)
  let editId = $state('')
  let form = $state({
    title: '', type_id: 0, event_date: '', locations: [''] as string[],
    has_gift: false, gift: '', summary: '', participant_ids: [] as string[],
    expense_yuan: '', expense_person_id: '',
  })

  function resetForm() {
    form = {
      title: '', type_id: 0, event_date: new Date().toISOString().slice(0, 10),
      locations: [''], has_gift: false, gift: '', summary: '', participant_ids: [],
      expense_yuan: '', expense_person_id: '',
    }
  }

  async function openEdit(e: Event) {
    const d = await API.get(`/api/v1/events/${e.id}`) as Event
    const own = (d.expenses || []).find(t => t.kind === 'expense' && t.direction === 'out')
    form = {
      title: d.title, type_id: d.type_id ?? 0, event_date: (d.event_date || '').slice(0, 10),
      locations: d.locations && d.locations.length > 0 ? [...d.locations] : [d.location || ''],
      has_gift: !!d.has_gift, gift: d.gift || '', summary: d.summary || '',
      participant_ids: (d.participants || []).map(p => p.id),
      expense_yuan: own ? (own.amount_fen / 100).toFixed(2) : (d.expense_fen ? (d.expense_fen / 100).toFixed(2) : ''),
      expense_person_id: own?.person_id || '',
    }
    editId = d.id
    showForm = true
  }

  async function load() {
    ;[list, types, people, timeline] = await Promise.all([
      API.get('/api/v1/events?limit=100'), API.get('/api/v1/event-types'),
      API.get('/api/v1/people?limit=500'), API.get('/api/v1/dashboard/timeline?limit=200'),
    ]) as any
  }
  onMount(load)

  async function submit() {
    if (!form.title.trim() || !form.event_date) { alert('标题和日期必填'); return }
    const locations = form.locations.map(s => s.trim()).filter(Boolean)
    const body: any = {
      title: form.title, event_date: form.event_date, locations,
      has_gift: form.has_gift, gift: form.has_gift ? form.gift.trim() : '',
      summary: form.summary, participant_ids: form.participant_ids,
    }
    if (form.type_id) body.type_id = form.type_id
    const expenseFen = Math.round(parseFloat(form.expense_yuan || '0') * 100)
    if (expenseFen > 0) {
      body.expense_fen = expenseFen
      if (form.expense_person_id) body.expense_person_id = form.expense_person_id
      else if (form.participant_ids.length === 0) { alert('填写开销时需要选择参与人或指定开销归属人'); return }
    } else if (editId) {
      body.expense_fen = 0 // 清空原有开销
    }
    if (editId) await API.put(`/api/v1/events/${editId}`, body)
    else await API.post('/api/v1/events', body)
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
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={() => { resetForm(); editId = ''; showForm = true }}>
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
    <ul class="space-y-2">
      {#each list as e}
        <li class="rounded-lg p-3 flex items-start gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
          <div class="w-2 h-2 rounded-full mt-2" style="background: {e.type_color || '#6366f1'};"></div>
          <div class="flex-1 min-w-0">
            <div class="flex items-start justify-between gap-2">
              <div class="text-sm font-medium">{e.title}</div>
              <button class="text-xs shrink-0 px-2 py-0.5 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-muted);" onclick={() => openEdit(e)}>编辑</button>
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
      {/each}
    </ul>
  {/if}
</div>

{#if showForm && !onlyTimeline}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4" style="background: rgba(0,0,0,0.3);" onclick={() => showForm = false}>
    <div class="w-full max-w-lg max-h-[90vh] overflow-auto rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);" onclick={(e) => e.stopPropagation()}>
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold">{editId ? '编辑往来' : '新建往来'}</h2>
        <button onclick={() => { showForm = false; editId = '' }}><X size={18} /></button>
      </div>
      <div class="space-y-3">
        <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" placeholder="标题" bind:value={form.title} />
        <div class="grid grid-cols-2 gap-3">
          <select bind:value={form.type_id} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
            <option value={0}>不选类型</option>
            {#each types as t}<option value={t.id}>{t.name}</option>{/each}
          </select>
          <input type="date" class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" bind:value={form.event_date} />
        </div>

        <div class="space-y-2">
          <div class="text-xs" style="color: var(--q-muted);">地点（可多个）</div>
          {#each form.locations as _, i}
            <div class="flex items-center gap-2">
              <input class="flex-1 px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" placeholder={i === 0 ? '地点，如 茶馆' : '追加地点'} bind:value={form.locations[i]} />
              {#if form.locations.length > 1}
                <button class="text-xs px-2 py-1 rounded" style="color: var(--q-muted);" onclick={() => form.locations = form.locations.filter((_, j) => j !== i)}>移除</button>
              {/if}
            </div>
          {/each}
          <button class="text-xs px-2 py-1 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => form.locations = [...form.locations, '']}>+ 添加地点</button>
        </div>

        <div class="flex items-center gap-2">
          <label class="flex items-center gap-1.5 text-sm shrink-0">
            <input type="checkbox" bind:checked={form.has_gift} /> 有礼物
          </label>
          {#if form.has_gift}
            <input class="flex-1 px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" placeholder="礼物说明，如 送了两盒茶" bind:value={form.gift} />
          {/if}
        </div>

        <div class="grid grid-cols-2 gap-3">
          <input type="number" step="0.01" min="0" class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" placeholder="开销（元，可空）" bind:value={form.expense_yuan} />
          <select bind:value={form.expense_person_id} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
            <option value="">开销归属（默认首位参与人）</option>
            {#each people as p}<option value={p.id}>{p.name}</option>{/each}
          </select>
        </div>

        <textarea class="w-full px-3 py-2 rounded-lg text-sm outline-none min-h-[80px]" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" placeholder="备注" bind:value={form.summary}></textarea>
        <div>
          <div class="text-xs mb-1" style="color: var(--q-muted);">参与人</div>
          <div class="flex flex-wrap gap-1">
            {#each people as p}
              <label class="flex items-center gap-1 text-xs px-2 py-1 rounded-md" style="background: var(--q-bg); border: 1px solid var(--q-border);">
                <input type="checkbox" value={p.id} bind:group={form.participant_ids} /> {p.name}
              </label>
            {/each}
          </div>
        </div>
      </div>
      <div class="flex justify-end gap-2 mt-5">
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => { showForm = false; editId = '' }}>取消</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submit}>保存</button>
      </div>
    </div>
  </div>
{/if}
