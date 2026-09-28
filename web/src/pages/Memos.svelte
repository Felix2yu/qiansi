<script lang="ts">
  import { onMount } from 'svelte'
  import { API, type Memo, type Person } from '../lib/api'
  import { Plus, X } from '@lucide/svelte'

  let list = $state<Memo[]>([])
  let people = $state<Person[]>([])
  let onlyPromises = $state(false)
  let showForm = $state(false)
  let form = $state({ person_id: '', speaker: 'other', content: '', said_at: '', is_promise: false, due_date: '', status: 'open' })

  async function load() {
    ;[list, people] = await Promise.all([
      API.get(`/api/v1/memos?promises_only=${onlyPromises ? 1 : 0}&limit=100`),
      API.get('/api/v1/people?limit=500'),
    ]) as any
  }
  onMount(load)
  async function submit() {
    if (!form.content.trim()) { alert('内容必填'); return }
    if (!form.said_at) form.said_at = new Date().toISOString().slice(0, 10)
    const body = { ...form }
    if (!body.person_id) delete body.person_id
    if (!body.due_date) delete body.due_date
    await API.post('/api/v1/memos', body); showForm = false; await load()
  }
  async function togglePromise(m: Memo) {
    m.is_promise = !m.is_promise; await API.put(`/api/v1/memos/${m.id}`, m); await load()
  }
  async function mark(m: Memo, status: string) { m.status = status; await API.put(`/api/v1/memos/${m.id}`, m); await load() }
  async function remove(id: string) { if (confirm('删除？')) { await API.delete(`/api/v1/memos/${id}`); await load() } }
  function personName(id?: string) { return people.find(p => p.id === id)?.name || '（未关联）' }
</script>
<div class="space-y-4">
  <header class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-semibold">对话 / 承诺</h1>
      <p class="text-sm mt-1" style="color: var(--q-muted);">记录重要的话与约定</p>
    </div>
    <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);">
            onclick={() => { form = { person_id:'', speaker:'other', content:'', said_at: new Date().toISOString().slice(0,10), is_promise:false, due_date:'', status:'open' }; showForm = true }}>
      <Plus size={14} /> 新建
    </button>
  </header>
  <div class="flex items-center gap-3">
    <label class="text-sm flex items-center gap-2" style="color: var(--q-muted);">
      <input type="checkbox" bind:checked={onlyPromises} onchange={load} /> 仅显示承诺
    </label>
  </div>
  <ul class="space-y-2">
    {#each list as m}
      <li class="rounded-lg p-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="flex items-start gap-2">
          <div class="flex-1 min-w-0 text-sm">{m.content}</div>
          <button class="text-xs" style="color: var(--q-muted);" onclick={() => remove(m.id)}>删除</button>
        </div>
        <div class="flex items-center gap-2 mt-2 text-xs" style="color: var(--q-muted);">
          {#if m.is_promise}<span class="px-2 py-0.5 rounded-full" style="background: #fef3c7; color: #92400e;">承诺</span>{/if}
          {#if m.status === 'open' && m.is_promise}<button onclick={() => mark(m, 'fulfilled')} style="color: #16a34a;">兑现</button>{/if}
          {#if m.status === 'open' && m.is_promise}<button onclick={() => mark(m, 'broken')} style="color: #dc2626;">未兑现</button>{/if}
          {#if m.status !== 'open'}<span class="px-2 py-0.5 rounded-full" style="background: var(--q-bg);">{m.status}</span>{/if}
          <span>{personName(m.person_id)} · {m.speaker} · {new Date(m.said_at).toLocaleDateString()}</span>
          {#if m.due_date}<span>到期 {new Date(m.due_date).toLocaleDateString()}</span>{/if}
          <button onclick={() => togglePromise(m)}>{m.is_promise ? '取消承诺' : '标记承诺'}</button>
        </div>
      </li>
    {:else}
      <li class="text-center py-8 text-sm" style="color: var(--q-muted);">暂无记录</li>
    {/each}
  </ul>
</div>

{#if showForm}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4" style="background: rgba(0,0,0,0.3);" onclick={() => showForm = false}>
    <div class="w-full max-w-lg rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold">新建</h2>
        <button onclick={() => showForm = false}><X size={18} /></button>
      </div>
      <div class="space-y-3">
        <select bind:value={form.person_id} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);">
          <option value="">选择联系人（可选）</option>
          {#each people as p}<option value={p.id}>{p.name}</option>{/each}
        </select>
        <div class="grid grid-cols-2 gap-3">
          <select bind:value={form.speaker} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);">
            <option value="other">对方说</option><option value="me">我说</option>
          </select>
          <input type="date" bind:value={form.said_at} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
        </div>
        <textarea class="w-full px-3 py-2 rounded-lg text-sm outline-none min-h-[100px]" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="对话 / 内容" bind:value={form.content}></textarea>
        <label class="text-sm flex items-center gap-2" style="color: var(--q-muted);"><input type="checkbox" bind:checked={form.is_promise} /> 这是一个承诺（可设置到期）</label>
        {#if form.is_promise}
          <input type="date" bind:value={form.due_date} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="到期日期（可选）" />
        {/if}
      </div>
      <div class="flex justify-end gap-2 mt-5">
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => showForm = false}>取消</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submit}>保存</button>
      </div>
    </div>
  </div>
{/if}
