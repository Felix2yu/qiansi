<script lang="ts">
  import { onMount } from 'svelte'
  import { API, type Event, type EventType, type Person, type TimelineItem } from '../lib/api'
  import { Plus, X } from '@lucide/svelte'

  let { onlyTimeline = false }: { onlyTimeline?: boolean } = $props()
  let list = $state<Event[]>([])
  let timeline = $state<TimelineItem[]>([])
  let types = $state<EventType[]>([])
  let people = $state<Person[]>([])
  let showForm = $state(false)
  let form = $state({ title: '', type_id: 0, event_date: '', location: '', summary: '', participant_ids: [] as string[] })

  async function load() {
    ;[list, types, people, timeline] = await Promise.all([
      API.get('/api/v1/events?limit=100'), API.get('/api/v1/event-types'),
      API.get('/api/v1/people?limit=500'), API.get('/api/v1/dashboard/timeline?limit=200'),
    ]) as any
  }
  onMount(load)

  async function submit() {
    if (!form.title.trim() || !form.event_date) { alert('标题和日期必填'); return }
    const body: any = { ...form }
    if (!body.type_id) delete body.type_id
    await API.post('/api/v1/events', body); showForm = false; await load()
  }
</script>
<div class="space-y-4">
  <header class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-semibold">{onlyTimeline ? '全局时间线' : '往来事件'}</h1>
      <p class="text-sm mt-1" style="color: var(--q-muted);">{onlyTimeline ? '所有类型混合视图' : '记录与亲友的见面、聚会、运动等'}</p>
    </div>
    {#if !onlyTimeline}
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={() => { form = { title:'', type_id:0, event_date: new Date().toISOString().slice(0,10), location:'', summary:'', participant_ids: [] }; showForm = true }}>
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
            <div class="text-sm font-medium">{e.title}</div>
            <div class="text-xs mt-0.5" style="color: var(--q-muted);">
              {e.type_name && `[${e.type_name}] `}
              {new Date(e.event_date).toLocaleDateString()}
              {e.location && ` · ${e.location}`}
              {e.participants && e.participants.length > 0 && ` · ${e.participants.map(p => p.name).join(', ')}`}
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
    <div class="w-full max-w-lg rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold">新建往来</h2>
        <button onclick={() => showForm = false}><X size={18} /></button>
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
        <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" placeholder="地点" bind:value={form.location} />
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
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => showForm = false}>取消</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submit}>保存</button>
      </div>
    </div>
  </div>
{/if}
