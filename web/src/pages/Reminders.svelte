<script lang="ts">
  import { onMount } from 'svelte'
  import { API, todayLocal, REF_TYPE_LABEL, type Reminder, type Person } from '../lib/api'
  import { Plus, X, Check } from '@lucide/svelte'

  let list = $state<Reminder[]>([])
  let people = $state<Person[]>([])
  let showDone = $state(false)
  let showForm = $state(false)
  let editId = $state('')
  let form = $state(emptyForm())

  function emptyForm() {
    return { title: '', due_at: todayLocal(), person_id: '', status: 'pending' }
  }

  // 后端按本地时间比较 due_at，这里不能再拼 UTC 的 Z，否则会整体偏移一天
  async function load() {
    ;[list, people] = await Promise.all([
      API.get(`/api/v1/reminders?status=${showDone ? 'done' : 'pending'}&limit=200`) as Promise<Reminder[]>,
      API.get('/api/v1/people?limit=500') as Promise<Person[]>,
    ])
  }
  onMount(load)

  function openCreate() {
    editId = ''
    form = emptyForm()
    showForm = true
  }

  function openEdit(r: Reminder) {
    if (r.id.startsWith('anniv:')) { alert('纪念日自动生成的提醒请到「纪念日」里调整'); return }
    editId = r.id
    form = {
      title: r.title, due_at: (r.due_at || '').slice(0, 10),
      person_id: r.person_id || '', status: r.status || 'pending',
    }
    showForm = true
  }

  async function submit() {
    if (!form.title.trim() || !form.due_at) { alert('内容和日期必填'); return }
    const body: any = { ...form, due_at: form.due_at + 'T09:00:00' }
    if (!body.person_id) delete body.person_id
    if (editId) await API.put(`/api/v1/reminders/${editId}`, body)
    else await API.post('/api/v1/reminders', body)
    showForm = false
    editId = ''
    await load()
  }

  // 按时间分区：逾期 / 今天 / 未来，避免逾期项与近期项混排后失去优先级
  const today = $derived(todayLocal())
  const groups = $derived.by(() => {
    if (showDone) return [{ key: 'done', label: '已完成', items: list }]
    const overdue: Reminder[] = [], dueToday: Reminder[] = [], upcoming: Reminder[] = []
    for (const r of list) {
      const d = (r.due_at || '').slice(0, 10)
      if (d && d < today) overdue.push(r)
      else if (d === today) dueToday.push(r)
      else upcoming.push(r)
    }
    return [
      { key: 'overdue', label: '已逾期', items: overdue },
      { key: 'today', label: '今天', items: dueToday },
      { key: 'upcoming', label: '之后', items: upcoming },
    ].filter(g => g.items.length > 0)
  })

  function overdueDays(r: Reminder) {
    const d = (r.due_at || '').slice(0, 10)
    if (!d) return 0
    const diff = Math.floor((new Date(today).getTime() - new Date(d).getTime()) / 86400000)
    return diff > 0 ? diff : 0
  }

  async function done(id: string) { await API.post(`/api/v1/reminders/${id}/done`, {}); await load() }
  async function undo(r: Reminder) {
    // PUT 是全量更新，必须带上原对象的其他字段，否则会被置空
    await API.put(`/api/v1/reminders/${r.id}`, { ...r, status: 'pending', completed_at: '' })
    await load()
  }
  async function remove(r: Reminder) {
    if (r.id.startsWith('anniv:')) { alert('纪念日提醒请到「纪念日」里删除'); return }
    if (confirm('删除？')) { await API.delete(`/api/v1/reminders/${r.id}`); await load() }
  }
</script>
<div class="space-y-4">
  <header class="flex items-center justify-between">
    <div><h1 class="text-2xl font-semibold">待办</h1><p class="text-sm mt-1" style="color: var(--q-muted);">自定义 & 自动生成</p></div>
    <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={openCreate}>
      <Plus size={14} /> 新建
    </button>
  </header>
  <div class="flex items-center gap-3">
    <label class="text-sm flex items-center gap-2" style="color: var(--q-muted);">
      <input type="checkbox" bind:checked={showDone} onchange={() => load()} /> 查看已完成
    </label>
  </div>
  {#if groups.length === 0}
    <div class="text-center py-8 text-sm" style="color: var(--q-muted);">{showDone ? '还没有已完成的待办' : '没有待办 🎉'}</div>
  {:else}
    {#each groups as g}
      <section class="space-y-2">
        <h2 class="text-xs font-medium flex items-center gap-2" style="color: {g.key === 'overdue' ? '#ef4444' : 'var(--q-muted)'};">
          {g.label}
          <span class="px-1.5 rounded-full" style="background: var(--q-surface); border: 1px solid var(--q-border);">{g.items.length}</span>
        </h2>
        <ul class="space-y-2">
          {#each g.items as r}
            <li class="rounded-lg p-3 flex items-center gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
              {#if r.status === 'pending'}
                <button class="w-6 h-6 rounded-md border flex items-center justify-center shrink-0" onclick={() => done(r.id)} style="border-color: var(--q-border);" title="标记完成">
                  <Check size={14} style="color: var(--q-muted);" />
                </button>
              {:else}
                <button class="w-6 h-6 rounded-md border flex items-center justify-center shrink-0" onclick={() => undo(r)} style="border-color: var(--q-border);" title="撤销完成">↩</button>
              {/if}
              <div class="flex-1 min-w-0">
                <div class="text-sm">{r.title}</div>
                <div class="text-xs mt-0.5" style="color: var(--q-muted);">
                  {REF_TYPE_LABEL[r.ref_type] || r.ref_type}{r.person_name && ' · ' + r.person_name} · {new Date(r.due_at).toLocaleDateString()}
                  {#if g.key === 'overdue'}· 已逾期 {overdueDays(r)} 天{/if}
                </div>
              </div>
              <button class="text-xs px-2 py-1 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => openEdit(r)}>编辑</button>
              <button class="text-xs" style="color: var(--q-muted);" onclick={() => remove(r)}>删</button>
            </li>
          {/each}
        </ul>
      </section>
    {/each}
  {/if}
</div>

{#if showForm}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4">
    <button type="button" aria-label="关闭弹窗" class="absolute inset-0 cursor-default" style="background: rgba(0,0,0,0.3); border: 0;" onclick={() => { showForm = false; editId = '' }}></button>
    <div class="relative w-full max-w-lg rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4"><h2 class="font-semibold">{editId ? '编辑待办' : '新建待办'}</h2><button onclick={() => { showForm = false; editId = '' }}><X size={18} /></button></div>
      <div class="space-y-3">
        <input bind:value={form.title} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="待办内容" />
        <input type="date" bind:value={form.due_at} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
        <select bind:value={form.person_id} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);">
          <option value="">关联联系人（可选）</option>
          {#each people as p}<option value={p.id}>{p.name}</option>{/each}
        </select>
      </div>
      <div class="flex justify-end gap-2 mt-5">
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => { showForm = false; editId = '' }}>取消</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submit}>保存</button>
      </div>
    </div>
  </div>
{/if}
