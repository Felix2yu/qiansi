<script lang="ts">
  import { onMount } from 'svelte';
  import { RefreshCcw, Trash2 } from '@lucide/svelte';
  import { API, contactAgo } from '../lib/api';
  import { toast } from '../lib/toast.svelte';
  import { ask } from '../lib/ask.svelte';
  import { TRASH_LABEL, restoreTrash, type TrashItem, type TrashKind } from '../lib/trash';
  import PageHeader from '../lib/ui/PageHeader.svelte';
  import Empty from '../lib/ui/Empty.svelte';

  let list = $state<TrashItem[]>([]);
  let loading = $state(true);

  async function load() {
    loading = true;
    try {
      list = await API.get<TrashItem[]>('/api/v1/trash');
    } catch (err) {
      toast.fail('加载回收站失败', err);
      list = [];
    } finally {
      loading = false;
    }
  }
  onMount(load);

  async function restore(it: TrashItem) {
    try {
      await restoreTrash(it.type, it.id);
    } catch (err) {
      toast.fail('恢复失败', err);
      return;
    }
    // 联系人还躺在回收站时，这条恢复了也仍不在列表里——不说明就是「点了没反应」
    toast.ok(
      it.type === 'person'
        ? it.hidden
          ? `已恢复，连带带回 ${it.hidden} 条记录`
          : '已恢复'
        : it.person_trashed
          ? '已恢复，但归属的联系人还在回收站，暂时不会出现在列表里'
          : '已恢复'
    );
    await load();
  }

  async function purge(it: TrashItem) {
    if (
      !(await ask({
        title: `彻底删除${TRASH_LABEL[it.type]}「${it.title}」？`,
        detail:
          it.type === 'person'
            ? '这个联系人的字段、关系与头像会一并删除，无法恢复。'
            : '这条记录将从数据库移除，无法恢复。',
        danger: true,
        confirmLabel: '彻底删除'
      }))
    )
      return;
    try {
      await API.delete(`/api/v1/trash/${it.type}/${it.id}`);
    } catch (err) {
      toast.fail('删除失败', err);
      return;
    }
    toast.ok('已彻底删除');
    await load();
  }

  async function emptyAll() {
    if (list.length === 0) return;
    if (
      !(await ask({
        title: '清空回收站？',
        detail: `${list.length} 条记录会被彻底删除，无法恢复。`,
        danger: true,
        confirmLabel: '清空'
      }))
    )
      return;
    try {
      const res = await API.delete<{ purged: Record<TrashKind, number> }>('/api/v1/trash');
      const n = Object.values(res?.purged ?? {}).reduce((a, b) => a + b, 0);
      toast.ok(`已清空，彻底删除 ${n} 条`);
    } catch (err) {
      toast.fail('清空失败', err);
      return;
    }
    await load();
  }

  function deletedTime(it: TrashItem) {
    return it.deleted_at ? new Date(it.deleted_at).toLocaleString() : '';
  }
</script>

<div class="q-page">
  <PageHeader title="回收站" subtitle="联系人、往来、对话、账目、纪念日与待办的删除都在这里">
    {#snippet actions()}
      {#if list.length > 0}
        <button class="q-btn q-btn--danger-ghost q-btn--sm" onclick={emptyAll}>
          <Trash2 size={14} /> 清空回收站
        </button>
      {/if}
    {/snippet}
  </PageHeader>

  {#if loading}
    <div class="py-12 text-center text-sm" style="color: var(--q-muted);">加载中…</div>
  {:else if list.length === 0}
    <Empty icon={Trash2} text="回收站是空的" />
  {:else}
    <div class="q-stack--tight flex flex-col">
      {#each list as it (it.type + it.id)}
        <div class="q-row">
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <span class="q-badge shrink-0">{TRASH_LABEL[it.type]}</span>
              <span class="truncate text-sm font-medium">{it.title}</span>
              {#if it.type === 'person' && it.hidden}
                <span class="q-badge shrink-0">恢复后带回 {it.hidden} 条</span>
              {/if}
              {#if it.type !== 'person' && it.person_trashed}
                <span class="q-badge q-badge--danger shrink-0">联系人还在回收站</span>
              {/if}
            </div>
            <div class="q-num mt-1 truncate text-xs" style="color: var(--q-muted);">
              {#if it.detail}{it.detail} ·
              {/if}
              {#if it.person_name}{it.person_name} ·
              {/if}
              删除于 {deletedTime(it)}（{contactAgo(it.deleted_at.slice(0, 10))}）
            </div>
          </div>
          <button class="q-btn q-btn--ghost q-btn--sm shrink-0" onclick={() => restore(it)}>
            <RefreshCcw size={12} /> 恢复
          </button>
          <button class="q-btn q-btn--danger-ghost q-btn--sm shrink-0" onclick={() => purge(it)}>
            彻底删除
          </button>
        </div>
      {/each}
    </div>
  {/if}
</div>
