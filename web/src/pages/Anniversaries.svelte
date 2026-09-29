<script lang="ts">
  import { onMount } from 'svelte'
  import { API, todayLocal, type Anniversary, type Person } from '../lib/api'
  import { Plus, X } from '@lucide/svelte'

  let list = $state<Anniversary[]>([])
  let people = $state<Person[]>([])
  let showForm = $state(false)
  let editId = $state('')
  let form = $state(emptyForm())

  function emptyForm() {
    return { person_id: '', title: '', date: todayLocal(), is_lunar: false, repeat_yearly: true, remind_days: '7,3,1,0' }
  }

  async function load() {
    ;[list, people] = await Promise.all([API.get('/api/v1/anniversaries'), API.get('/api/v1/people?limit=500')]) as any
  }
  onMount(load)

  function openCreate() {
    editId = ''
    form = emptyForm()
    showForm = true
  }

  function openEdit(a: Anniversary) {
    editId = a.id
    form = {
      person_id: a.person_id || '', title: a.title, date: (a.date || '').slice(0, 10),
      is_lunar: !!a.is_lunar, repeat_yearly: !!a.repeat_yearly, remind_days: a.remind_days || '7,3,1,0',
    }
    showForm = true
  }

  async function submit() {
    if (!form.title.trim() || !form.date) { alert('标题和日期必填'); return }
    const body: any = { ...form }
    if (!body.person_id) delete body.person_id
    if (!body.remind_days.trim()) body.remind_days = '0'
    if (editId) await API.put(`/api/v1/anniversaries/${editId}`, body)
    else await API.post('/api/v1/anniversaries', body)
    showForm = false
    editId = ''
    await load()
  }

  async function remove(a: Anniversary) {
    if (!confirm(`删除「${a.title}」？`)) return
    await API.delete(`/api/v1/anniversaries/${a.id}`); await load()
  }

  /** 每年重复的纪念日只有月日有意义，展示时补上今年的年份便于阅读 */
  function displayDate(a: Anniversary) {
    const d = new Date(a.date)
    return a.repeat_yearly ? `${d.getMonth() + 1}月${d.getDate()}日` : d.toLocaleDateString()
  }

  /** 角标显示下一次发生的月日（循环/农历项取后端换算后的公历日期） */
  function badgeDate(a: Anniversary) {
    const d = new Date(a.next_date || a.date)
    return { m: d.getMonth() + 1, d: d.getDate() }
  }

  /** 倒计时文案；null 表示不展示 chip */
  function countdownLabel(a: Anniversary): string | null {
    if (a.days_until == null) return a.repeat_yearly ? null : '已过'
    if (a.days_until === 0) return '今天'
    return `${a.days_until} 天`
  }
</script>
<div class="space-y-4">
  <header class="flex items-center justify-between">
    <div><h1 class="text-2xl font-semibold">纪念日</h1><p class="text-sm mt-1" style="color: var(--q-muted);">生日、周年、倒数日</p></div>
    <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={openCreate}>
      <Plus size={14} /> 新建
    </button>
  </header>
  <ul class="space-y-2">
    {#each list as a}
      {@const bd = badgeDate(a)}
      {@const cd = countdownLabel(a)}
      <li class="rounded-lg p-3 flex items-center gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="w-10 h-10 rounded-lg flex flex-col items-center justify-center text-white shrink-0" style="background: var(--q-theme);">
          <div class="text-[10px] leading-none">{bd.m}月</div>
          <div class="text-base font-semibold leading-none mt-0.5">{bd.d}</div>
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-sm font-medium truncate">{a.title}</div>
          <div class="text-xs mt-0.5" style="color: var(--q-muted);">
            {a.person_name && `${a.person_name} · `}
            {a.is_lunar ? '农历 ' : ''}{displayDate(a)}
            {a.repeat_yearly ? ' · 每年循环' : ''}
          </div>
        </div>
        {#if cd}
          <span class="text-xs px-2 py-0.5 rounded-full shrink-0"
            style={a.days_until != null && a.days_until <= 7
              ? 'background: var(--q-theme); color: #fff;'
              : 'background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-muted);'}>{cd}</span>
        {/if}
        <button class="text-xs px-2 py-1 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => openEdit(a)}>编辑</button>
        <button class="text-xs" style="color: var(--q-muted);" onclick={() => remove(a)}>删</button>
      </li>
    {:else}<li class="text-center py-8 text-sm" style="color: var(--q-muted);">暂无</li>{/each}
  </ul>
</div>

{#if showForm}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4">
    <button type="button" aria-label="关闭弹窗" class="absolute inset-0 cursor-default" style="background: rgba(0,0,0,0.3); border: 0;" onclick={() => { showForm = false; editId = '' }}></button>
    <div class="relative w-full max-w-lg rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4"><h2 class="font-semibold">{editId ? '编辑纪念日' : '新建纪念日'}</h2><button onclick={() => { showForm = false; editId = '' }}><X size={18} /></button></div>
      <div class="space-y-3">
        <select bind:value={form.person_id} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);">
          <option value="">自属（倒数日）</option>{#each people as p}<option value={p.id}>{p.name}</option>{/each}
        </select>
        <input bind:value={form.title} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="标题（如：妈妈生日）" />
        <input type="date" bind:value={form.date} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
        <div class="flex items-center gap-4">
          <label class="text-sm flex items-center gap-2" style="color: var(--q-muted);"><input type="checkbox" bind:checked={form.is_lunar} /> 农历</label>
          <label class="text-sm flex items-center gap-2" style="color: var(--q-muted);"><input type="checkbox" bind:checked={form.repeat_yearly} /> 每年循环</label>
        </div>
        <input bind:value={form.remind_days} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="提醒天数（逗号分隔，如 7,3,1,0）" />
      </div>
      <div class="flex justify-end gap-2 mt-5">
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => { showForm = false; editId = '' }}>取消</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submit}>保存</button>
      </div>
    </div>
  </div>
{/if}
