<script lang="ts">
  import { onMount } from 'svelte'
  import { API, yuan, toFen, todayLocal, KIND_LABEL, DIRECTION_LABEL,
           type Transaction, type Person, type Event, type Repayment } from '../lib/api'
  import { Plus, X, Trash2 } from '@lucide/svelte'

  let list = $state<Transaction[]>([])
  let people = $state<Person[]>([])
  let events = $state<Event[]>([])
  let showForm = $state(false)
  let editId = $state('')
  let form = $state(emptyForm())
  // 还款面板
  let repayOf = $state<Transaction | null>(null)
  let repayList = $state<Repayment[]>([])
  let repayAmount = $state('')
  let repayDate = $state(todayLocal())
  let repayNote = $state('')

  function emptyForm() {
    return { person_id: '', kind: 'loan', direction: 'out', amount_yuan: '', title: '',
             occurred_at: todayLocal(), due_date: '', event_id: '', settled: false }
  }

  const PAGE = 50
  let page = $state(0)
  let hasMore = $state(false)

  async function load(reset = true) {
    if (reset) page = 0
    const [batch, pe, ev] = await Promise.all([
      API.get(`/api/v1/transactions?limit=${PAGE}&offset=${page * PAGE}`),
      API.get('/api/v1/people?limit=500'), API.get('/api/v1/events?limit=100')
    ]) as any
    hasMore = (batch || []).length === PAGE
    list = reset ? batch : [...list, ...batch]
    people = pe; events = ev
  }
  async function loadMore() {
    page += 1
    await load(false)
  }
  onMount(() => load(true))

  function openCreate() {
    editId = ''
    form = emptyForm()
    showForm = true
  }

  async function openEdit(t: Transaction) {
    const full = await API.get(`/api/v1/transactions/${t.id}`) as Transaction
    editId = full.id
    form = {
      person_id: full.person_id, kind: full.kind, direction: full.direction,
      amount_yuan: yuan(full.amount_fen), title: full.title || '',
      occurred_at: (full.occurred_at || '').slice(0, 10),
      due_date: full.due_date ? full.due_date.slice(0, 10) : '',
      event_id: full.event_id || '', settled: !!full.settled,
    }
    showForm = true
  }

  async function submit() {
    const fen = toFen(form.amount_yuan)
    if (!form.person_id || fen <= 0) { alert('请选择联系人并填写大于 0 的金额（元）'); return }
    const body: any = {
      person_id: form.person_id, kind: form.kind, direction: form.direction,
      amount_fen: fen, title: form.title, occurred_at: form.occurred_at || todayLocal(),
      due_date: form.due_date || '', event_id: form.event_id || '', settled: !!form.settled,
    }
    if (editId) await API.put(`/api/v1/transactions/${editId}`, body)
    else await API.post('/api/v1/transactions', body)
    showForm = false
    editId = ''
    await load()
  }

  async function remove(id: string) { if (confirm('删除这笔记录？')) { await API.delete(`/api/v1/transactions/${id}`); await load() } }

  function personName(id: string) { return people.find(p => p.id === id)?.name || '?' }
  function remaining(t: Transaction) { return Math.max(0, t.amount_fen - (t.repaid_fen || 0)) }

  async function openRepay(t: Transaction) {
    repayOf = t
    repayAmount = ''
    repayDate = todayLocal()
    repayNote = ''
    repayList = await API.get(`/api/v1/transactions/${t.id}/repayments`) as Repayment[]
  }

  async function submitRepay() {
    if (!repayOf) return
    const fen = toFen(repayAmount)
    if (fen <= 0) { alert('请填写还款金额（元）'); return }
    await API.post(`/api/v1/transactions/${repayOf.id}/repayments`, {
      amount_fen: fen, occurred_at: repayDate || todayLocal(), note: repayNote,
    })
    // 还清则自动标记为结清
    const fresh = await API.get(`/api/v1/transactions/${repayOf.id}`) as Transaction
    if (remaining(fresh) === 0 && !fresh.settled) {
      await API.put(`/api/v1/transactions/${fresh.id}`, { ...fresh, settled: true })
    }
    repayAmount = ''
    repayNote = ''
    repayList = await API.get(`/api/v1/transactions/${repayOf.id}/repayments`) as Repayment[]
    await load()
    repayOf = await API.get(`/api/v1/transactions/${repayOf.id}`) as Transaction
  }

  async function removeRepay(id: string) {
    if (!repayOf) return
    if (!confirm('删除这条还款记录？')) return
    await API.delete(`/api/v1/transactions/${repayOf.id}/repayments/${id}`)
    repayList = await API.get(`/api/v1/transactions/${repayOf.id}/repayments`) as Repayment[]
    await load()
    repayOf = await API.get(`/api/v1/transactions/${repayOf.id}`) as Transaction
  }

  // 只翻转结清标记（用于无需逐笔登记还款的场景）
  async function toggleSettled(id: string) {
    const t = await API.get(`/api/v1/transactions/${id}`) as Transaction
    await API.put(`/api/v1/transactions/${id}`, { ...t, settled: !t.settled })
    await load()
  }
</script>
<div class="space-y-4">
  <header class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-semibold">金钱往来</h1>
      <p class="text-sm mt-1" style="color: var(--q-muted);">借款、还款、礼物、花销</p>
    </div>
    <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={openCreate}>
      <Plus size={14} /> 新建
    </button>
  </header>
  <ul class="space-y-2">
    {#each list as t}
      <li class="rounded-lg p-3 flex items-center gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="w-10 h-10 rounded-full flex items-center justify-center text-sm font-semibold shrink-0"
             style="background: {t.direction === 'out' ? 'rgba(239,68,68,0.15)' : 'rgba(16,185,129,0.15)'}; color: {t.direction === 'out' ? '#ef4444' : '#10b981'};">
          {t.direction === 'out' ? '出' : '入'}
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-sm font-medium truncate">{personName(t.person_id)}{t.title && ` · ${t.title}`}</div>
          <div class="text-xs mt-0.5" style="color: var(--q-muted);">
            ¥{yuan(t.amount_fen)} · {KIND_LABEL[t.kind] || t.kind} · {DIRECTION_LABEL[t.direction] || t.direction} · {new Date(t.occurred_at).toLocaleDateString()}
            {t.settled ? ' · 已结清' : ''}
            {#if !t.settled && (t.repaid_fen || 0) > 0}· 已还 ¥{yuan(t.repaid_fen || 0)}，待还 ¥{yuan(remaining(t))}{/if}
            {t.event_title && ` · 关联事件：${t.event_title}`}
          </div>
        </div>
        <div class="text-sm font-semibold shrink-0" style="color: {t.direction === 'out' ? '#ef4444' : '#10b981'};">{t.direction === 'out' ? '-' : '+'}¥{yuan(t.amount_fen)}</div>
        <div class="flex gap-1 shrink-0">
          {#if t.kind === 'loan'}
            <button class="text-xs px-2 py-1 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => openRepay(t)}>还款</button>
          {/if}
          <button class="text-xs px-2 py-1 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => toggleSettled(t.id)}>{t.settled ? '取消结清' : '结清'}</button>
          <button class="text-xs px-2 py-1 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => openEdit(t)}>编辑</button>
          <button class="text-xs" style="color: var(--q-muted);" onclick={() => remove(t.id)}>删</button>
        </div>
      </li>
    {:else}
      <li class="text-center py-8 text-sm" style="color: var(--q-muted);">暂无记录</li>
    {/each}
  </ul>
  {#if hasMore}
    <div class="text-center">
      <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-muted);" onclick={loadMore}>
        加载更多（已显示 {list.length} 条）
      </button>
    </div>
  {/if}
</div>

{#if showForm}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4">
    <button type="button" aria-label="关闭弹窗" class="absolute inset-0 cursor-default" style="background: rgba(0,0,0,0.3); border: 0;" onclick={() => { showForm = false; editId = '' }}></button>
    <div class="relative w-full max-w-lg rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4"><h2 class="font-semibold">{editId ? '编辑' : '新建'}</h2><button onclick={() => { showForm = false; editId = '' }}><X size={18} /></button></div>
      <div class="space-y-3">
        <select bind:value={form.person_id} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);">
          <option value="">选择联系人</option>{#each people as p}<option value={p.id}>{p.name}</option>{/each}
        </select>
        <div class="grid grid-cols-2 gap-3">
          <select bind:value={form.kind} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);">
            <option value="loan">借还</option><option value="gift">礼物</option><option value="expense">花销</option><option value="other">其它</option>
          </select>
          <select bind:value={form.direction} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);">
            <option value="out">我支出</option><option value="in">我收入</option>
          </select>
        </div>
        <input type="number" step="0.01" min="0" bind:value={form.amount_yuan} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="金额（元）" />
        <input bind:value={form.title} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="标题（如：生日礼物）" />
        <div class="grid grid-cols-2 gap-3">
          <input type="date" bind:value={form.occurred_at} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
          <input type="date" bind:value={form.due_date} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="到期（可选）" />
        </div>
        <select bind:value={form.event_id} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);">
          <option value="">关联事件（可选）</option>
          {#each events as e}<option value={e.id}>{new Date(e.event_date).toLocaleDateString()} · {e.title}</option>{/each}
        </select>
        <label class="text-sm flex items-center gap-2" style="color: var(--q-muted);"><input type="checkbox" bind:checked={form.settled} /> 已结清</label>
      </div>
      <div class="flex justify-end gap-2 mt-5">
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => { showForm = false; editId = '' }}>取消</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submit}>保存</button>
      </div>
    </div>
  </div>
{/if}

{#if repayOf}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4">
    <button type="button" aria-label="关闭弹窗" class="absolute inset-0 cursor-default" style="background: rgba(0,0,0,0.3); border: 0;" onclick={() => (repayOf = null)}></button>
    <div class="relative w-full max-w-lg rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold">还款流水 · {personName(repayOf.person_id)}</h2>
        <button onclick={() => repayOf = null}><X size={18} /></button>
      </div>
      <div class="text-sm mb-3" style="color: var(--q-muted);">
        本金 ¥{yuan(repayOf.amount_fen)} · 已还 ¥{yuan(repayOf.repaid_fen || 0)} · 待还 <span style="color: var(--q-text);">¥{yuan(remaining(repayOf))}</span>
      </div>
      <ul class="space-y-1 mb-3 max-h-48 overflow-auto">
        {#each repayList as r}
          <li class="flex items-center gap-2 text-sm px-2 py-1 rounded" style="background: var(--q-bg);">
            <span>¥{yuan(r.amount_fen)}</span>
            <span style="color: var(--q-muted);">{r.occurred_at}{r.note ? ` · ${r.note}` : ''}</span>
            <button class="ml-auto" style="color: var(--q-muted);" onclick={() => removeRepay(r.id)}><Trash2 size={14} /></button>
          </li>
        {:else}
          <li class="text-sm px-2 py-1" style="color: var(--q-muted);">还没有还款记录</li>
        {/each}
      </ul>
      <div class="grid grid-cols-3 gap-2">
        <input type="number" step="0.01" min="0" bind:value={repayAmount} placeholder="本次还款（元）" class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
        <input type="date" bind:value={repayDate} class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
        <input bind:value={repayNote} placeholder="备注（可选）" class="px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
      </div>
      <div class="flex justify-end gap-2 mt-4">
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => repayOf = null}>关闭</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submitRepay}>记一笔</button>
      </div>
    </div>
  </div>
{/if}
