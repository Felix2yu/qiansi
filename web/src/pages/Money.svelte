<script lang="ts">
  import { onMount } from 'svelte'
  import { API, type Transaction, type Person } from '../lib/api'
  import { Plus, X } from '@lucide/svelte'

  let list = $state<Transaction[]>([])
  let people = $state<Person[]>([])
  let showForm = $state(false)
  let form = $state({ person_id: '', kind: 'loan', direction: 'out', amount_fen: 0, title: '', occurred_at: '', due_date: '' })

  async function load() {
    ;[list, people] = await Promise.all([
      API.get('/api/v1/transactions?limit=200'), API.get('/api/v1/people?limit=500')
    ]) as any
  }
  onMount(load)
  async function submit() {
    if (!form.person_id || !form.amount_fen) { alert('必填'); return }
    if (!form.occurred_at) form.occurred_at = new Date().toISOString().slice(0, 10)
    await API.post('/api/v1/transactions', form); showForm = false; await load()
  }
  async function markSettled(id: string) {
    const t = await API.get(`/api/v1/transactions/${id}`) as Transaction
    t.settled = !t.settled
    await API.put(`/api/v1/transactions/${id}`, t); await load()
  }
  async function remove(id: string) { if (confirm('删除？')) { await API.delete(`/api/v1/transactions/${id}`); await load() } }
  function yuan(fen: number) { return '¥' + (fen / 100).toFixed(2) }
  function personName(id: string) { return people.find(p => p.id === id)?.name || '?' }
</script>
<div class="space-y-4">
  <header class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-semibold">金钱往来</h1>
      <p class="text-sm mt-1" style="color: var(--q-muted);">借款、还款、礼物、花销</p>
    </div>
    <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);">
            onclick={() => { form = { person_id:'', kind:'loan', direction:'out', amount_fen:0, title:'', occurred_at: new Date().toISOString().slice(0,10), due_date:'' }; showForm = true }}>
      <Plus size={14} /> 新建
    </button>
  </header>
  <ul class="space-y-2">
    {#each list as t}
      <li class="rounded-lg p-3 flex items-center gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="w-10 h-10 rounded-full flex items-center justify-center text-sm font-semibold"
             style="background: {t.direction === 'out' ? 'rgba(239,68,68,0.15)' : 'rgba(16,185,129,0.15)'}; color: {t.direction === 'out' ? '#ef4444' : '#10b981'};">
          {t.direction === 'out' ? '借' : '入'}
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-sm font-medium truncate">{personName(t.person_id)}{t.title && ` · ${t.title}`}</div>
          <div class="text-xs mt-0.5" style="color: var(--q-muted);">
            {yuan(t.amount_fen)} · {t.kind} · {new Date(t.occurred_at).toLocaleDateString()}
            {t.settled && ' · 已结清'}
          </div>
        </div>
        <div class="text-sm font-semibold" style="color: {t.direction === 'out' ? '#ef4444' : '#10b981'};">{t.direction === 'out' ? '-' : '+'}{yuan(t.amount_fen)}</div>
        <div class="flex gap-1">
          {#if !t.settled}<button class="text-xs px-2 py-1 rounded" style="background: var(--q-bg);" onclick={() => markSettled(t.id)}>结清</button>{/if}
          <button class="text-xs" style="color: var(--q-muted);" onclick={() => remove(t.id)}>删</button>
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
      <div class="flex items-center justify-between mb-4"><h2 class="font-semibold">新建</h2><button onclick={() => showForm = false}><X size={18} /></button></div>
      <div class="space-y-3">
        <select bind:value={form.person_id} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);">
          <option value="">选择联系人</option>{#each people as p}<option value={p.id}>{p.name}</option>{/each}
        </select>
        <div class="grid grid-cols-2 gap-3">
          <select bind:value={form.kind} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);">
            <option value="loan">借/还</option><option value="gift">礼物</option><option value="expense">花销</option><option value="other">其它</option>
          </select>
          <select bind:value={form.direction} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);">
            <option value="out">我借出/支出</option><option value="in">我入账/收到</option>
          </select>
        </div>
        <input type="number" bind:value={form.amount_fen} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="金额（分）" />
        <input bind:value={form.title} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="标题（如：生日礼物）" />
        <div class="grid grid-cols-2 gap-3">
          <input type="date" bind:value={form.occurred_at} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
          <input type="date" bind:value={form.due_date} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="到期（可选）" />
        </div>
      </div>
      <div class="flex justify-end gap-2 mt-5">
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => showForm = false}>取消</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submit}>保存</button>
      </div>
    </div>
  </div>
{/if}
