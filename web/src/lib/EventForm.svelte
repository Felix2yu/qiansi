<script lang="ts">
  import { untrack } from 'svelte'
  import { API, todayLocal, type Event, type EventType, type Person } from './api'
  import { selfFirst, personLabel, loadSelf, isSelf } from './self.svelte'
  import { Trash2, Search } from '@lucide/svelte'

  let {
    editId = '',
    preset = {},
    types = null,
    people = null,
    onsave,
    oncancel,
    onremove,
  }: {
    /** 非空进入编辑模式，组件自行拉取详情 */
    editId?: string
    /** 新建时的预填：默认日期、默认参与人 */
    preset?: { event_date?: string; participant_ids?: string[] }
    /** 传入可复用父级已加载的数据，不传则自行请求 */
    types?: EventType[] | null
    people?: Person[] | null
    onsave?: () => void
    oncancel?: () => void
    /** 提供后编辑态显示删除按钮，由父级确认并删除 */
    onremove?: (id: string, title: string, expenseFen?: number) => void
  } = $props()

  let localTypes = $state<EventType[]>([])
  let localPeople = $state<Person[]>([])
  const typeList = $derived(types ?? localTypes)
  const peopleList = $derived(people ?? localPeople)

  let loading = $state(false)
  let saving = $state(false)
  // 编辑时原有关联开销，用于删除时提示是否解挂账目
  let originExpenseFen = $state(0)
  // 参与人较多时不能平铺全量，这里按关键字过滤
  let peopleQuery = $state('')

  type Form = {
    title: string; type_id: number; event_date: string
    locations: string[]; has_gift: boolean; gift: string; summary: string
    participant_ids: string[]; expense_yuan: string; expense_person_id: string
  }

  function blank(): Form {
    return {
      title: '', type_id: 0, event_date: preset?.event_date || todayLocal(),
      locations: [''], has_gift: false, gift: '', summary: '',
      participant_ids: [...(preset?.participant_ids || [])],
      expense_yuan: '', expense_person_id: '',
    }
  }

  let form = $state<Form>(blank())

  // 切换新建 / 编辑目标时刷新表单；seq 用于丢弃过期的详情响应
  let loadSeq = 0
  $effect(() => {
    if (editId) void loadDetail(editId)
    else { form = blank(); originExpenseFen = 0; peopleQuery = '' }
  })

  // 父级没传数据时自行拉取，避免每个调用方都写一遍（只看初始值，故用 untrack）
  untrack(() => {
    void loadSelf()
    if (!types) void API.get('/api/v1/event-types').then(r => (localTypes = r as EventType[])).catch(() => {})
    if (!people) void API.get('/api/v1/people?limit=500').then(r => (localPeople = r as Person[])).catch(() => {})
  })

  async function loadDetail(id: string) {
    const seq = ++loadSeq
    loading = true
    try {
      const d = await API.get(`/api/v1/events/${id}`) as Event
      if (seq !== loadSeq) return
      const own = (d.expenses || []).find(t => t.kind === 'expense' && t.direction === 'out')
      originExpenseFen = d.expense_fen || own?.amount_fen || 0
      form = {
        title: d.title, type_id: d.type_id ?? 0, event_date: (d.event_date || '').slice(0, 10),
        locations: d.locations && d.locations.length > 0 ? [...d.locations] : [d.location || ''],
        has_gift: !!d.has_gift, gift: d.gift || '', summary: d.summary || '',
        participant_ids: (d.participants || []).map(p => p.id),
        expense_yuan: own ? (own.amount_fen / 100).toFixed(2) : (d.expense_fen ? (d.expense_fen / 100).toFixed(2) : ''),
        expense_person_id: own?.person_id || '',
      }
      peopleQuery = ''
    } catch (err: any) {
      alert('读取往来失败：' + (err?.message || err))
      oncancel?.()
    } finally {
      if (seq === loadSeq) loading = false
    }
  }

  const matchedPeople = $derived(
    peopleQuery.trim()
      ? selfFirst(peopleList.filter(p => p.name.includes(peopleQuery.trim()) || (p.nickname || '').includes(peopleQuery.trim())))
      : selfFirst(peopleList)
  )
  const participants = $derived(peopleList.filter(p => form.participant_ids.includes(p.id)))

  function toggleParticipant(id: string) {
    form.participant_ids = form.participant_ids.includes(id)
      ? form.participant_ids.filter(x => x !== id)
      : [...form.participant_ids, id]
  }

  // 常见场景的一键预填：只填「骨架」，明细仍由用户补
  const TEMPLATES = [
    { name: '见面聊天', title: '见面', summary: '聊了聊近况', has_gift: false },
    { name: '一起吃饭', title: '吃饭', summary: '', has_gift: false },
    { name: '送礼', title: '送礼', summary: '', has_gift: true, gift: '' },
    { name: '电话/微信', title: '通话', summary: '', has_gift: false },
    { name: '运动出游', title: '一起运动', summary: '', has_gift: false },
  ]

  function applyTemplate(t: typeof TEMPLATES[number]) {
    form = {
      ...form,
      title: t.title,
      summary: form.summary || t.summary,
      has_gift: t.has_gift,
      gift: t.has_gift ? (form.gift || t.gift || '') : '',
    }
  }

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
    saving = true
    try {
      if (editId) await API.put(`/api/v1/events/${editId}`, body)
      else await API.post('/api/v1/events', body)
      onsave?.()
    } catch (err: any) {
      alert('保存失败：' + (err?.message || err))
    } finally {
      saving = false
    }
  }
</script>

<div class="space-y-3" class:opacity-60={loading}>
  {#if loading}
    <div class="text-center text-sm py-6" style="color: var(--q-muted);">加载中…</div>
  {:else}
    {#if !editId}
      <div class="flex flex-wrap gap-1">
        {#each TEMPLATES as t}
          <button class="text-xs px-2 py-1 rounded-md" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-muted);"
                  onclick={() => applyTemplate(t)}>{t.name}</button>
        {/each}
      </div>
    {/if}
    <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" placeholder="标题" bind:value={form.title} />
    <div class="grid grid-cols-2 gap-3">
      <select bind:value={form.type_id} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
        <option value={0}>不选类型</option>
        {#each typeList as t}<option value={t.id}>{t.name}</option>{/each}
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
        {#each selfFirst(participants) as p}<option value={p.id}>{personLabel(p)}（参与人）</option>{/each}
        {#each selfFirst(peopleList.filter(p => !form.participant_ids.includes(p.id))) as p}<option value={p.id}>{personLabel(p)}</option>{/each}
      </select>
    </div>

    <textarea class="w-full px-3 py-2 rounded-lg text-sm outline-none min-h-[80px]" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" placeholder="备注" bind:value={form.summary}></textarea>
    <div>
      <div class="flex items-center justify-between mb-1">
        <div class="text-xs" style="color: var(--q-muted);">参与人（已选 {form.participant_ids.length} 人）</div>
        <div class="relative">
          <Search size={12} class="absolute left-2 top-1/2 -translate-y-1/2" style="color: var(--q-muted);" />
          <input bind:value={peopleQuery} placeholder="搜索联系人"
                 class="pl-7 pr-2 py-1 rounded-md text-xs outline-none w-36"
                 style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" />
        </div>
      </div>
      <div class="flex flex-wrap gap-1 max-h-40 overflow-auto">
        {#each matchedPeople as p}
          <button class="flex items-center gap-1 text-xs px-2 py-1 rounded-md transition"
                  style={form.participant_ids.includes(p.id)
                    ? 'background: var(--q-theme); color: white; border: 1px solid var(--q-theme);'
                    : isSelf(p.id)
                      ? 'background: var(--q-bg); color: #f59e0b; border: 1px solid #f59e0b;'
                      : 'background: var(--q-bg); color: var(--q-text); border: 1px solid var(--q-border);'}
                  onclick={() => toggleParticipant(p.id)}>{personLabel(p)}</button>
        {:else}
          <span class="text-xs" style="color: var(--q-muted);">没有匹配的联系人</span>
        {/each}
      </div>
    </div>
  {/if}
</div>

<div class="flex items-center justify-between gap-2 mt-5">
  {#if editId && onremove}
    <button class="flex items-center gap-1 px-3 py-2 rounded-lg text-sm" style="border: 1px solid var(--q-border); color: #ef4444;"
            onclick={() => onremove?.(editId, form.title, originExpenseFen)}>
      <Trash2 size={14} /> 删除
    </button>
  {:else}
    <span></span>
  {/if}
  <div class="flex gap-2">
    <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => oncancel?.()}>取消</button>
    <button class="px-4 py-2 rounded-lg text-sm text-white disabled:opacity-60" style="background: var(--q-theme);" disabled={saving || loading} onclick={submit}>
      {saving ? '保存中…' : '保存'}
    </button>
  </div>
</div>
