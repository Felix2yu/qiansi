<script lang="ts">
  import { API, type Category, type Tag, type EventType } from './api'
  import { Plus, Pencil, Trash2, X } from '@lucide/svelte'

  // 圈子 / 标签 / 事件类型三处结构相同：id + name + color，其余字段编辑时原样带回
  type Term = Category | Tag | EventType

  let {
    items,
    endpoint,
    noun,
    placeholder,
    pill = false,
    defaults = null,
    onchange,
  }: {
    items: Term[]
    endpoint: string
    /** 提示文案里的名称，如「圈子」 */
    noun: string
    placeholder: string
    /** 标签用胶囊排布，其余用整行 */
    pill?: boolean
    /** 后端 PUT/POST 是整体覆盖，icon 这类界面不暴露的字段从这里补 */
    defaults?: Record<string, unknown> | null
    onchange: () => void
  } = $props()

  let draft = $state({ name: '', color: '#6366f1' })
  let editingId = $state(0)
  let editForm = $state({ name: '', color: '' })
  let busy = $state(false)

  const editing = $derived(items.find(t => t.id === editingId))

  function warn(err: any, action: string) {
    const msg = String(err?.message || err)
    // tags.name 上有 UNIQUE 约束，撞名的 sqlite 原文不适合直接甩给他看
    alert(`${action}失败：` + (/UNIQUE/i.test(msg) ? `${noun}名已存在` : msg))
  }

  async function create() {
    if (!draft.name.trim()) return
    busy = true
    try {
      await API.post(endpoint, { ...defaults, name: draft.name.trim(), color: draft.color })
      draft = { name: '', color: draft.color }
      await onchange()
    } catch (err) {
      warn(err, '添加')
    } finally { busy = false }
  }

  function startEdit(t: Term) {
    editingId = t.id
    editForm = { name: t.name, color: t.color }
  }

  async function save() {
    if (!editing) return
    if (!editForm.name.trim()) { alert(`${noun}名不能为空`); return }
    busy = true
    try {
      await API.put(`${endpoint}/${editing.id}`, { ...defaults, ...editing, name: editForm.name.trim(), color: editForm.color })
      editingId = 0
      await onchange()
    } catch (err) {
      warn(err, '保存')
    } finally { busy = false }
  }

  async function remove(t: Term) {
    if (!confirm(`删除${noun}「${t.name}」？`)) return
    busy = true
    try {
      await API.delete(`${endpoint}/${t.id}`)
      if (editingId === t.id) editingId = 0
      await onchange()
    } catch (err) {
      warn(err, '删除')
    } finally { busy = false }
  }
</script>

<ul class={pill ? 'flex flex-wrap gap-2 mb-3' : 'space-y-1 mb-3'}>
  {#each items as t (t.id)}
    {#if pill}
      <li class="flex items-center gap-1 text-xs px-2 py-1 rounded-full"
          style="background: color-mix(in srgb, {t.color} 15%, transparent); color: {t.color};">
        {t.name}
        <button title="编辑" onclick={() => startEdit(t)}><Pencil size={12} /></button>
        <button title="删除" onclick={() => remove(t)}><Trash2 size={12} /></button>
      </li>
    {:else}
      <li class="flex items-center gap-2 text-sm px-2 py-1 rounded-md" style="background: var(--q-bg);">
        <span class="w-3 h-3 rounded-full shrink-0" style="background: {t.color};"></span>
        <span class="truncate">{t.name}</span>
        <span class="ml-auto flex items-center gap-1 shrink-0">
          <button class="p-0.5" style="color: var(--q-muted);" title="编辑" onclick={() => startEdit(t)}><Pencil size={14} /></button>
          <button class="p-0.5" style="color: var(--q-muted);" title="删除" onclick={() => remove(t)}><Trash2 size={14} /></button>
        </span>
      </li>
    {/if}
  {:else}
    <li class="text-sm" style="color: var(--q-muted);">还没有{noun}</li>
  {/each}
</ul>

<div class="flex gap-2">
  {#if editing}
    <input bind:value={editForm.name} class="flex-1 px-3 py-2 rounded-lg text-sm outline-none"
           style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" placeholder="{noun}名" />
    <input type="color" bind:value={editForm.color} class="w-10 h-10 rounded-lg border" />
    <button class="px-3 py-2 rounded-lg text-sm text-white disabled:opacity-60" style="background: var(--q-theme);"
            disabled={busy} onclick={save}>保存</button>
    <button class="px-2 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-muted);"
            title="取消" onclick={() => (editingId = 0)}><X size={14} /></button>
  {:else}
    <input bind:value={draft.name} class="flex-1 px-3 py-2 rounded-lg text-sm outline-none"
           style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);"
           placeholder={placeholder} onkeydown={(e) => e.key === 'Enter' && create()} />
    <input type="color" bind:value={draft.color} class="w-10 h-10 rounded-lg border" />
    <button class="flex items-center gap-1 px-3 py-2 rounded-lg text-sm text-white disabled:opacity-60"
            style="background: var(--q-theme);" disabled={busy} onclick={create}><Plus size={14} /> 添加</button>
  {/if}
</div>
