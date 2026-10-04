<script lang="ts">
  import { API, RELATION_TYPES, type Relationship } from './api'
  import { self, loadSelf, isSelf } from './self.svelte'
  import { dict, ensure } from './dict.svelte'
  import PersonPicker from './PersonPicker.svelte'
  import { Trash2, Plus, Pencil, X, Check } from '@lucide/svelte'

  // 关系维护：以 personId 为中心列出全部关系，支持添加、行内改类型/备注、删除
  let {
    personId,
    onchange,
  }: {
    personId: string
    onchange?: () => void
  } = $props()

  let rels = $state<Relationship[]>([])
  // 历史上已用过的关系类型（闺蜜、对象、挚友…），与预设合并后喂给 datalist
  let usedTypes = $state<string[]>([])
  let loading = $state(true)
  let showAdd = $state(false)
  let addForm = $state({ to_person_id: '', type: RELATION_TYPES[0], remark: '' })
  let editingId = $state('')
  let editForm = $state({ type: RELATION_TYPES[0], remark: '' })
  let busy = $state(false)

  // 同一页面可能挂两个编辑器（详情区 + 编辑弹窗），datalist id 按人物区分避免重复
  const listId = $derived(`rel-type-options-${personId}`)
  const typeOptions = $derived(
    Array.from(new Set([...RELATION_TYPES, ...usedTypes, ...rels.map(r => r.type).filter(Boolean)])))

  // 人物变化时重新拉取；不依赖 rels，避免自己改完又反复请求
  $effect(() => {
    const pid = personId
    if (!pid) { rels = []; loading = false; return }
    load(pid)
  })

  async function load(pid = personId) {
    loading = true
    try {
      const [list, types] = await Promise.all([
        API.get<Relationship[]>(`/api/v1/relationships/of/${pid}`),
        ensure('relTypes').then(() => dict.relTypes),
      ])
      // 同一对的人可以并存几条不同类型的边，按对方名字排一下才看得出是连着同一个人
      rels = (list || []).sort((a, b) =>
        otherName(a).localeCompare(otherName(b), 'zh') || a.type.localeCompare(b.type, 'zh'))
      usedTypes = types || []
    } catch {
      rels = []
    } finally { loading = false }
  }

  function otherName(r: Relationship) {
    const otherId = r.from_person_id === personId ? r.to_person_id : r.from_person_id
    const name = r.from_person_id === personId ? (r.to_name || '?') : (r.from_name || '?')
    return isSelf(otherId) ? `我 · ${name}` : name
  }

  async function openAdd() {
    showAdd = true
    await loadSelf()
    // 给别人补关系时，最常见的另一端就是我自己
    if (!addForm.to_person_id && self.id && self.id !== personId) addForm.to_person_id = self.id
  }

  // 关系是有向的，from 永远是当前人物；换人只改备注与类型
  async function add() {
    const type = addForm.type.trim()
    if (!addForm.to_person_id) { alert('请选择对方'); return }
    if (addForm.to_person_id === personId) { alert('不能与自己建立关系'); return }
    if (!type) { alert('请填写关系类型'); return }
    busy = true
    try {
      await API.post('/api/v1/relationships', {
        from_person_id: personId, to_person_id: addForm.to_person_id,
        type, remark: addForm.remark,
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
    const type = editForm.type.trim()
    if (!type) { alert('请填写关系类型'); return }
    busy = true
    try {
      await API.put(`/api/v1/relationships/${r.id}`, {
        from_person_id: r.from_person_id, to_person_id: r.to_person_id,
        type, remark: editForm.remark,
      })
      cancelEdit()
      await load()
      onchange?.()
    } catch (err: any) {
      alert('保存失败：' + (err?.message || err))
    } finally { busy = false }
  }

  async function remove(r: Relationship) {
    // 同一对人间现在可以并存几条不同类型的边，只写名字会分不清删的是哪条
    if (!confirm(`删除与「${otherName(r)}」的「${r.type}」关系？`)) return
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

<datalist id={listId}>
  {#each typeOptions as t}<option value={t}></option>{/each}
</datalist>

<div class="space-y-3">
  <div class="flex items-center justify-between">
    <span class="text-xs" style="color: var(--q-muted);">
      {loading ? '加载中…' : rels.length > 0 ? `已维护 ${rels.length} 条` : '还没有维护关系'}
    </span>
    {#if !showAdd}
      <button class="flex items-center gap-1 text-xs px-2 py-1 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border);"
              onclick={openAdd}>
        <Plus size={12} /> 添加关系
      </button>
    {/if}
  </div>

  {#if showAdd}
    <div class="rounded-lg p-2 space-y-2" style="background: var(--q-bg);">
      <div class="grid grid-cols-2 gap-2">
        <PersonPicker bind:value={addForm.to_person_id} exclude={[personId]} placeholder="选择对方…" clearable={false} />
        <input list={listId} bind:value={addForm.type} placeholder="关系类型（可自定义）"
               class="w-full px-2 py-2 rounded-lg text-sm outline-none" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);" />
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
            <input list={listId} bind:value={editForm.type} placeholder="关系类型"
                   class="w-28 px-2 py-1 rounded-lg text-xs outline-none" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);" />
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
