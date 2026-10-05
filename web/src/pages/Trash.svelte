<script lang="ts">
  import { onMount } from 'svelte'
  import { RefreshCcw, Trash2 } from '@lucide/svelte'
  import { API, contactAgo } from '../lib/api'
  import { toast } from '../lib/toast.svelte'
  import { ask } from '../lib/ask.svelte'
  import { TRASH_LABEL, restoreTrash, type TrashItem, type TrashKind } from '../lib/trash'

  let list = $state<TrashItem[]>([])
  let loading = $state(true)

  async function load() {
    loading = true
    try {
      list = await API.get<TrashItem[]>('/api/v1/trash')
    } catch (err) {
      toast.fail('加载回收站失败', err)
      list = []
    } finally {
      loading = false
    }
  }
  onMount(load)

  async function restore(it: TrashItem) {
    try {
      await restoreTrash(it.type, it.id)
    } catch (err) {
      toast.fail('恢复失败', err)
      return
    }
    // 联系人还躺在回收站时，这条恢复了也仍不在列表里——不说明就是「点了没反应」
    toast.ok(it.type === 'person'
      ? (it.hidden ? `已恢复，连带带回 ${it.hidden} 条记录` : '已恢复')
      : it.person_trashed ? '已恢复，但归属的联系人还在回收站，暂时不会出现在列表里' : '已恢复')
    await load()
  }

  async function purge(it: TrashItem) {
    if (!(await ask({
      title: `彻底删除${TRASH_LABEL[it.type]}「${it.title}」？`,
      detail: it.type === 'person'
        ? '这个联系人的字段、关系与头像会一并删除，无法恢复。'
        : '这条记录将从数据库移除，无法恢复。',
      danger: true,
      confirmLabel: '彻底删除',
    }))) return
    try {
      await API.delete(`/api/v1/trash/${it.type}/${it.id}`)
    } catch (err) {
      toast.fail('删除失败', err)
      return
    }
    toast.ok('已彻底删除')
    await load()
  }

  async function emptyAll() {
    if (list.length === 0) return
    if (!(await ask({
      title: '清空回收站？',
      detail: `${list.length} 条记录会被彻底删除，无法恢复。`,
      danger: true,
      confirmLabel: '清空',
    }))) return
    try {
      const res = await API.delete<{ purged: Record<TrashKind, number> }>('/api/v1/trash')
      const n = Object.values(res?.purged ?? {}).reduce((a, b) => a + b, 0)
      toast.ok(`已清空，彻底删除 ${n} 条`)
    } catch (err) {
      toast.fail('清空失败', err)
      return
    }
    await load()
  }

  function deletedTime(it: TrashItem) {
    return it.deleted_at ? new Date(it.deleted_at).toLocaleString() : ''
  }
</script>

<div class="space-y-4">
  <header class="flex items-center justify-between gap-3">
    <div>
      <h1 class="text-2xl font-semibold">回收站</h1>
      <p class="text-sm mt-1" style="color: var(--q-muted);">联系人、往来、对话、账目、纪念日与待办的删除都在这里</p>
    </div>
    {#if list.length > 0}
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm"
              style="background: var(--q-bg); border: 1px solid var(--q-border); color: #ef4444;"
              onclick={emptyAll}>
        <Trash2 size={14} /> 清空回收站
      </button>
    {/if}
  </header>

  {#if loading}
    <div class="text-center py-8 text-sm" style="color: var(--q-muted);">加载中…</div>
  {:else if list.length === 0}
    <div class="text-center py-10 text-sm" style="color: var(--q-muted);">回收站是空的</div>
  {:else}
    <ul class="space-y-2">
      {#each list as it (it.type + it.id)}
        <li class="rounded-lg p-3 flex items-center gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2 flex-wrap">
              <span class="text-[10px] px-1.5 py-0.5 rounded shrink-0" style="background: var(--q-bg); color: var(--q-muted);">{TRASH_LABEL[it.type]}</span>
              <span class="text-sm truncate">{it.title}</span>
              {#if it.type === 'person' && it.hidden}
                <span class="text-[10px] px-1.5 py-0.5 rounded" style="background: var(--q-bg); color: var(--q-muted);">恢复后带回 {it.hidden} 条</span>
              {/if}
              {#if it.type !== 'person' && it.person_trashed}
                <span class="text-[10px] px-1.5 py-0.5 rounded" style="background: var(--q-bg); color: #ef4444;">联系人还在回收站</span>
              {/if}
            </div>
            <div class="text-xs mt-1" style="color: var(--q-muted);">
              {#if it.detail}{it.detail} · {/if}
              {#if it.person_name}{it.person_name} · {/if}
              删除于 {deletedTime(it)}（{contactAgo(it.deleted_at.slice(0, 10))}）
            </div>
          </div>
          <button class="flex items-center gap-1 text-xs px-2.5 py-1.5 rounded-lg shrink-0"
                  style="background: var(--q-bg); border: 1px solid var(--q-border);"
                  onclick={() => restore(it)}>
            <RefreshCcw size={12} /> 恢复
          </button>
          <button class="text-xs px-2.5 py-1.5 rounded-lg shrink-0"
                  style="background: var(--q-bg); border: 1px solid var(--q-border); color: #ef4444;"
                  onclick={() => purge(it)}>彻底删除</button>
        </li>
      {/each}
    </ul>
  {/if}
</div>
