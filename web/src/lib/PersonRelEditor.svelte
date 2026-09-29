<script lang="ts">
  import { API, RELATION_TYPES, type Person, type Relationship } from './api'
  import { Trash2, Plus, Pencil, X, Check } from '@lucide/svelte'

  // 关系维护：以 personId 为中心列出全部关系，支持添加、行内改类型/备注、删除
  let {
    personId,
    people = null,
    onchange,
  }: {
    personId: string
    /** 外部已加载的候选人列表；不传则组件自己拉 */
    people?: Person[] | null
    onchange?: () => void
  } = $props()

  let rels = $state<Relationship[]>([])
  let options = $state<Person[]>([])
  let loading = $state(true)
  let showAdd = $state(false)
  let addForm = $state({ to_person_id: '', type: RELATION_TYPES[0], remark: '' })
  let editingId = $state('')
  let editForm = $state({ type: RELATION_TYPES[0], remark: '' })
  let busy = $state(false)

  // 人物或候选人变化时重新拉取；只依赖这两个，避免自己改 rels 时反复请求
  $effect(() => {
    const pid = personId
    if (!pid) { rels = []; loading = false; return }
    load(pid)
  })

  $effect(() => {
    if (people) options = people
  })

  async function load(pid = personId) {
    loading = true
    try {
      const list = await API.get<Relationship[]>(`/api/v1/relationships/of/${pid}`)
      rels = list || []
    } catch {
      rels = []
    } finally {
      loading = false
    }
  }

  async function loadOptions() {
    try {
      options = await API.get<Person[]>('/api/v1/people?limit=500')
    } catch { options = [] }
  }

  function candidates(excludeId: string) {
    return options.filter(p => p.id !== excludeId)
  }

  function otherName(r: Relationship) {
    return r.from_person_id === personId ? (r.to_name || '?') : (r.from_name || '?')
  }

  // 关系是有向的，from 永远是当前人物；换人只改备注与类型
  async function add() {
    if (!addForm.to_person_id) { alert('请选择对方'); return }
    if (addForm.to_person_id === personId) { alert('不能与自己建立关系'); return }
    busy = true
    try {
      await API.post('/api/v1/relationships', {
        from_person_id: personId, to_person_id: addForm.to_person_id,
        type: addForm.type, remark: addForm.remark,
      })
      showAdd = false
      addForm = { to_person_id: '', type: RELATION_TYPES[0], remark: '' }
      await load()
      onchange?.()
    } catch (err: any) {
      alert('添加失败：' + (err?.message || err))
    } finally { busy = false }
  }

  function startEdit(r: Relationship) {
    editingId = r.id
    editForm = { type: r.type, remark: r.remark || '' }
  }

  function cancelEdit() {
    editingId = ''
    editForm = { type: RELATION_TYPES[0], remark: '' }
  }

  async function saveEdit(r: Relationship) {
    busy = true
    try {
      await API.put(`/api/v1/relationships/${r.id}`, {
        from_person_id: r.from_person_id, to_person_id: r.to_person_id,
        type: editForm.type, remark: editForm.remark,
      })
      cancelEdit()
      await load()
      onchange?.()
    } catch (err: any) {
      alert('保存失败：' + (err?.message || err))
    } finally { busy = false }
  }

  async function remove(r: Relationship) {
    if (!confirm(`删除与「${otherName(r)}」的关系？`)) return
    try {
      await API.delete(`/api/v1/relationships/${r.id}`)
      if (editingId === r.id) cancelEdit()
      await load()
      onchange?.()
    } catch (err: any) {
      alert('删除失败：' + (err?.message || err))
    }
  }
</script>

<div class="space-y-3">
  <div class="flex items-center justify-between">
    <span class="text-xs" style="color: var(--q-muted);">
      {loading ? '加载中…' : rels.length > 0 ? `已维护 ${rels.length} 条` : '还没有维护关系'}
    </span>
    {#if !showAdd}
      <button class="flex items-center gap-1 text-xs px-2 py-1 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border);"
              onclick={async () => { showAdd = true; if (!people) await loadOptions() }}>
        <Plus size={12} /> 添加关系
      </button>
    {/if}
  </div>

  {#if showAdd}
    <div class="rounded-lg p-2 space-y-2" style="background: var(--q-bg);">
      <div class="grid grid-cols-2 gap-2">
        <select bind:value={addForm.to_person_id} class="w-full px-2 py-2 rounded-lg text-sm outline-none" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);">
          <option value="">选择对方…</option>
          {#each candidates(personId) as p}<option value={p.id}>{p.name}</option>{/each}
        </select>
        <select bind:value={addForm.type} class="w-full px-2 py-2 rounded-lg text-sm outline-none" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);">
          {#each RELATION_TYPES as t}<option value={t}>{t}</option>{/each}
        </select>
      </div>
      <div class="flex gap-2">
        <input bind:value={addForm.remark} placeholder="备注（可选）" class="flex-1 px-2 py-2 rounded-lg text-sm outline-none" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);" />
        <button class="px-3 py-2 rounded-lg text-sm text-white disabled:opacity-60" style="background: var(--q-theme);" disabled={busy} onclick={add}>保存</button>
        <button class="px-2 py-2 rounded-lg text-sm" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-muted);"
                title="取消" onclick={() => (showAdd = false)}><X size={14} /></button>
      </div>
    </div>
  {/if}

  <ul class="space-y-1">
    {#each rels as r (r.id)}
      <li class="px-2 py-1.5 rounded-md text-sm" style="background: var(--q-bg);">
        {#if editingId === r.id}
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-xs" style="color: var(--q-muted);">{otherName(r)}</span>
            <select bind:value={editForm.type} class="px-2 py-1 rounded-lg text-xs outline-none" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);">
              {#each RELATION_TYPES as t}<option value={t}>{t}</option>{/each}
            </select>
            <input bind:value={editForm.remark} placeholder="备注" class="flex-1 min-w-24 px-2 py-1 rounded-lg text-xs outline-none" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);" />
            <button class="p-1 rounded disabled:opacity-60" style="color: var(--q-theme);" title="保存" disabled={busy} onclick={() => saveEdit(r)}><Check size={14} /></button>
            <button class="p-1 rounded" style="color: var(--q-muted);" title="取消" onclick={cancelEdit}><X size={14} /></button>
          </div>
        {:else}
          <div class="flex items-center gap-2">
            <span class="text-xs px-1.5 py-0.5 rounded shrink-0" style="background: var(--q-surface); color: var(--q-muted);">{r.type}</span>
            <span class="truncate">{otherName(r)}</span>
            {#if r.remark}<span class="truncate text-xs" style="color: var(--q-muted);">· {r.remark}</span>{/if}
            <span class="ml-auto flex items-center gap-1 shrink-0">
              <button class="p-1 rounded" style="color: var(--q-muted);" title="编辑" onclick={() => startEdit(r)}><Pencil size={14} /></button>
              <button class="p-1 rounded" style="color: var(--q-muted);" title="删除" onclick={() => remove(r)}><Trash2 size={14} /></button>
            </span>
          </div>
        {/if}
      </li>
    {:else}
      {#if !loading && !showAdd}
        <li class="text-sm" style="color: var(--q-muted);">点右上角「添加关系」，把 TA 连进来</li>
      {/if}
    {/each}
  </ul>
</div>
