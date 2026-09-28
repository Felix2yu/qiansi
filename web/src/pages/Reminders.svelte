<script lang="ts">
  import { onMount } from 'svelte'
  import { API, type Reminder } from '../lib/api'
  import { Plus, X, Check } from '@lucide/svelte'
  let list = $state<Reminder[]>([])
  let showForm = $state(false)
  let form = $state({ title: '', due_at: '', status: 'pending' })
  async function load() { list = await API.get('/api/v1/reminders?status=pending&limit=200') as Reminder[] }
  onMount(load)
  async function submit() { if (!form.title || !form.due_at) { alert('必填'); return }
    await API.post('/api/v1/reminders', { ...form, due_at: form.due_at + 'T09:00:00Z' }); showForm = false; await load() }
  async function done(id: string) { await API.post(`/api/v1/reminders/${id}/done`, {}); await load() }
  async function remove(id: string) { if (confirm('删除？')) { await API.delete(`/api/v1/reminders/${id}`); await load() } }
</script>
<div class="space-y-4">
  <header class="flex items-center justify-between">
    <div><h1 class="text-2xl font-semibold">待办</h1><p class="text-sm mt-1" style="color: var(--q-muted);">自定义 & 自动生成</p></div>
    <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={() => { form = { title:'', due_at: new Date().toISOString().slice(0,10), status:'pending' }; showForm = true }}>
      <Plus size={14} /> 新建
    </button>
  </header>
  <ul class="space-y-2">
    {#each list as r}
      <li class="rounded-lg p-3 flex items-center gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <button class="w-6 h-6 rounded-md border flex items-center justify-center" onclick={() => done(r.id)} style="border-color: var(--q-border);">
          <Check size={14} style="color: var(--q-muted);" />
        </button>
        <div class="flex-1 min-w-0">
          <div class="text-sm">{r.title}</div>
          <div class="text-xs mt-0.5" style="color: var(--q-muted);">{r.ref_type}{r.person_name && ' · ' + r.person_name} · {new Date(r.due_at).toLocaleDateString()}</div>
        </div>
        <button class="text-xs" style="color: var(--q-muted);" onclick={() => remove(r.id)}>删</button>
      </li>
    {:else}<li class="text-center py-8 text-sm" style="color: var(--q-muted);">没有待办 🎉</li>{/each}
  </ul>
</div>

{#if showForm}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4" style="background: rgba(0,0,0,0.3);" onclick={() => showForm = false}>
    <div class="w-full max-w-lg rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4"><h2 class="font-semibold">新建待办</h2><button onclick={() => showForm = false}><X size={18} /></button></div>
      <div class="space-y-3">
        <input bind:value={form.title} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="待办内容" />
        <input type="date" bind:value={form.due_at} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
      </div>
      <div class="flex justify-end gap-2 mt-5">
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => showForm = false}>取消</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submit}>保存</button>
      </div>
    </div>
  </div>
{/if}
