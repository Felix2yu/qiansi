<script lang="ts">
  import { onMount } from 'svelte'
  import { API, MEMO_STATUS_LABEL, todayLocal, type Memo } from '../lib/api'
  import { Plus, X } from '@lucide/svelte'
  import { personLabel, loadSelf } from '../lib/self.svelte'
  import PersonPicker from '../lib/PersonPicker.svelte'
  import { toast } from '../lib/toast.svelte'
  import { ask } from '../lib/ask.svelte'
  import { RECOVER_NOTE, trashOne } from '../lib/trash'

  let list = $state<Memo[]>([])
  let onlyPromises = $state(false)
  let showForm = $state(false)
  let editId = $state('')
  let form = $state(emptyForm())
  // 编辑时把列表里带的名字交给选人控件显示，省一次回查
  let pickedName = $state('')

  function emptyForm() {
    return { person_id: '', speaker: 'other', content: '', said_at: todayLocal(), is_promise: false, due_date: '', status: 'open' }
  }

  const PAGE = 50
  let page = $state(0)
  let hasMore = $state(false)

  async function load(reset = true) {
    if (reset) page = 0
    const batch = await API.get<Memo[]>(
      `/api/v1/memos?promises_only=${onlyPromises ? 1 : 0}&limit=${PAGE}&offset=${page * PAGE}`
    ).catch((err: any) => {
      // 读失败也不能装作「暂无记录」：空列表和查不到是两件事
      toast.fail('加载失败', err)
      return [] as Memo[]
    })
    hasMore = batch.length === PAGE
    list = reset ? batch : [...list, ...batch]
  }
  async function loadMore() {
    page += 1
    await load(false)
  }
  onMount(() => {
    loadSelf()
    load(true)
  })

  function openCreate() {
    editId = ''
    form = emptyForm()
    pickedName = ''
    showForm = true
  }

  function openEdit(m: Memo) {
    editId = m.id
    pickedName = m.person_name || ''
    form = {
      person_id: m.person_id || '', speaker: m.speaker || 'other', content: m.content,
      said_at: (m.said_at || '').slice(0, 10), is_promise: !!m.is_promise,
      due_date: m.due_date ? m.due_date.slice(0, 10) : '', status: m.status || 'open',
    }
    showForm = true
  }

  async function submit() {
    if (!form.content.trim()) { toast.error('内容必填'); return }
    if (!form.said_at) form.said_at = todayLocal()
    const body: any = { ...form }
    if (!body.person_id) delete body.person_id
    if (!body.due_date) delete body.due_date
    const isEdit = !!editId
    try {
      if (isEdit) await API.put(`/api/v1/memos/${editId}`, body)
      else await API.post('/api/v1/memos', body)
    } catch (err: any) {
      // 失败时弹窗不关、内容不丢，改完可以直接再存一次
      toast.fail('保存失败', err)
      return
    }
    showForm = false
    editId = ''
    toast.ok(isEdit ? '已更新' : '已保存')
    await load()
  }

  async function togglePromise(m: Memo) {
    const was = !!m.is_promise
    const snapshot = { ...m }
    try {
      await API.put(`/api/v1/memos/${m.id}`, { ...snapshot, is_promise: !was })
      toast.undoable(was ? '已取消承诺标记' : '已标记为承诺', async () => {
        await API.put(`/api/v1/memos/${m.id}`, { ...snapshot, is_promise: was }).catch(() => {})
        await load()
      })
      await load()
    } catch (err: any) {
      toast.fail('保存失败', err)
    }
  }

  async function mark(m: Memo, status: string) {
    const was = m.status
    const snapshot = { ...m }
    try {
      await API.put(`/api/v1/memos/${m.id}`, { ...snapshot, status })
      toast.undoable(`已标记为「${MEMO_STATUS_LABEL[status] || status}」`, async () => {
        await API.put(`/api/v1/memos/${m.id}`, { ...snapshot, status: was }).catch(() => {})
        await load()
      })
      await load()
    } catch (err: any) {
      toast.fail('保存失败', err)
    }
  }

  async function remove(m: Memo) {
    if (!(await ask({ title: '删除这条对话记录？', detail: RECOVER_NOTE, danger: true, confirmLabel: '删除' }))) return
    await trashOne('memo', m.id, load, '已删除')
  }
  function memoWho(m: Memo) {
    if (!m.person_id) return '（未关联）'
    // 名字随列表一起回来，不再为了这几个字拉全量名单
    return personLabel({ id: m.person_id, name: m.person_name || '（已删除）' })
  }
</script>
<div class="space-y-4">
  <header class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-semibold">对话 / 承诺</h1>
      <p class="text-sm mt-1" style="color: var(--q-muted);">记录重要的话与约定</p>
    </div>
    <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={openCreate}>
      <Plus size={14} /> 新建
    </button>
  </header>
  <div class="flex items-center gap-3">
    <label class="text-sm flex items-center gap-2" style="color: var(--q-muted);">
      <input type="checkbox" bind:checked={onlyPromises} onchange={() => load()} /> 仅显示承诺
    </label>
  </div>
  <ul class="space-y-2">
    {#each list as m}
      <li class="rounded-lg p-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="flex items-start gap-2">
          <div class="flex-1 min-w-0 text-sm">{m.content}</div>
          <button class="text-xs px-2 py-0.5 rounded shrink-0" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-muted);" onclick={() => openEdit(m)}>编辑</button>
          <button class="text-xs shrink-0" style="color: var(--q-muted);" onclick={() => remove(m)}>删除</button>
        </div>
        <div class="flex items-center gap-2 mt-2 text-xs" style="color: var(--q-muted);">
          {#if m.is_promise}<span class="px-2 py-0.5 rounded-full" style="background: #fef3c7; color: #92400e;">承诺</span>{/if}
          {#if m.status === 'open' && m.is_promise}<button onclick={() => mark(m, 'fulfilled')} style="color: var(--q-ok);">兑现</button>{/if}
          {#if m.status === 'open' && m.is_promise}<button onclick={() => mark(m, 'broken')} style="color: var(--q-danger);">未兑现</button>{/if}
          {#if m.status !== 'open'}<span class="px-2 py-0.5 rounded-full" style="background: var(--q-bg);">{MEMO_STATUS_LABEL[m.status] || m.status}</span>
            <!-- 兑现/未兑现已定论后仍要能改回来：否则一次误点就永久盖住了这条承诺 -->
            <button onclick={() => mark(m, 'open')}>改回待办</button>{/if}
          <span>{memoWho(m)} · {m.speaker === 'me' ? '我说' : '对方说'} · {new Date(m.said_at).toLocaleDateString()}</span>
          {#if m.due_date}<span>到期 {new Date(m.due_date).toLocaleDateString()}</span>{/if}
          <button onclick={() => togglePromise(m)}>{m.is_promise ? '取消承诺' : '标记承诺'}</button>
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
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold">{editId ? '编辑' : '新建'}</h2>
        <button onclick={() => { showForm = false; editId = '' }}><X size={18} /></button>
      </div>
      <div class="space-y-3">
        <PersonPicker bind:value={form.person_id} selectedName={pickedName} placeholder="选择联系人（可选）" />
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
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => { showForm = false; editId = '' }}>取消</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submit}>保存</button>
      </div>
    </div>
  </div>
{/if}
