<script lang="ts">
  import { onMount } from 'svelte'
  import { API, type Anniversary, type Person } from '../lib/api'
  import { Plus, X } from '@lucide/svelte'

  let list = $state<Anniversary[]>([])
  let people = $state<Person[]>([])
  let showForm = $state(false)
  let form = $state({ person_id: '', title: '', date: '', is_lunar: false, repeat_yearly: true, remind_days: '7,3,1,0' })

  async function load() {
    ;[list, people] = await Promise.all([API.get('/api/v1/anniversaries'), API.get('/api/v1/people?limit=500')]) as any
  }
  onMount(load)
  async function submit() {
    if (!form.title || !form.date) { alert('必填'); return }
    await API.post('/api/v1/anniversaries', form); showForm = false; await load()
  }
  async function remove(id: string) { if (confirm('删除？')) { await API.delete(`/api/v1/anniversaries/${id}`); await load() } }
</script>
<div class="space-y-4">
  <header class="flex items-center justify-between">
    <div><h1 class="text-2xl font-semibold">纪念日</h1><p class="text-sm mt-1" style="color: var(--q-muted);">生日、周年、倒数日</p></div>
    <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={() => { form = { person_id:'', title:'', date:'', is_lunar:false, repeat_yearly:true, remind_days:'7,3,1,0' }; showForm = true }}>
      <Plus size={14} /> 新建
    </button>
  </header>
  <ul class="space-y-2">
    {#each list as a}
      <li class="rounded-lg p-3 flex items-center gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="w-10 h-10 rounded-lg flex flex-col items-center justify-center text-white shrink-0" style="background: var(--q-theme);">
          <div class="text-[10px] leading-none">{new Date(a.date).getMonth()+1}月</div>
          <div class="text-base font-semibold leading-none mt-0.5">{new Date(a.date).getDate()}</div>
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-sm font-medium truncate">{a.title}</div>
          <div class="text-xs mt-0.5" style="color: var(--q-muted);">
            {a.person_name && `· ${a.person_name} `}
            {a.is_lunar && '农历 '}{new Date(a.date).toLocaleDateString()}
            {a.repeat_yearly && ' · 每年循环'}
          </div>
        </div>
        <button class="text-xs" style="color: var(--q-muted);" onclick={() => remove(a.id)}>删</button>
      </li>
    {:else}<li class="text-center py-8 text-sm" style="color: var(--q-muted);">暂无</li>{/each}
  </ul>
</div>

{#if showForm}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4" style="background: rgba(0,0,0,0.3);" onclick={() => showForm = false}>
    <div class="w-full max-w-lg rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4"><h2 class="font-semibold">新建纪念日</h2><button onclick={() => showForm = false}><X size={18} /></button></div>
      <div class="space-y-3">
        <select bind:value={form.person_id} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);">
          <option value="">自属（倒数日）</option>{#each people as p}<option value={p.id}>{p.name}</option>{/each}
        </select>
        <input bind:value={form.title} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="标题（如：妈妈生日）" />
        <input type="date" bind:value={form.date} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
        <label class="text-sm flex items-center gap-2" style="color: var(--q-muted);"><input type="checkbox" bind:checked={form.is_lunar} /> 农历</label>
        <label class="text-sm flex items-center gap-2" style="color: var(--q-muted);"><input type="checkbox" bind:checked={form.repeat_yearly} /> 每年循环</label>
        <input bind:value={form.remind_days} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="提醒天数（逗号分隔，如 7,3,1,0）" />
      </div>
      <div class="flex justify-end gap-2 mt-5">
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => showForm = false}>取消</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submit}>保存</button>
      </div>
    </div>
  </div>
{/if}
