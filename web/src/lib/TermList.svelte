<script lang="ts">
  import { API, type Category, type Tag, type EventType } from './api';
  import { Plus, Pencil, Trash2, X } from '@lucide/svelte';
  import { toast, errText } from './toast.svelte';
  import { ask } from './ask.svelte';

  // 圈子 / 标签 / 事件类型三处结构相同：id + name + color，其余字段编辑时原样带回
  type Term = Category | Tag | EventType;

  let {
    items,
    endpoint,
    noun,
    placeholder,
    pill = false,
    defaults = null,
    onchange
  }: {
    items: Term[];
    endpoint: string;
    /** 提示文案里的名称，如「圈子」 */
    noun: string;
    placeholder: string;
    /** 标签用胶囊排布，其余用整行 */
    pill?: boolean;
    /** 后端 PUT/POST 是整体覆盖，icon 这类界面不暴露的字段从这里补 */
    defaults?: Record<string, unknown> | null;
    onchange: () => void;
  } = $props();

  let draft = $state({ name: '', color: '#9E2A2B' });
  let editingId = $state(0);
  let editForm = $state({ name: '', color: '' });
  let busy = $state(false);

  const editing = $derived(items.find((t) => t.id === editingId));

  function warn(err: unknown, action: string) {
    const msg = errText(err);
    // tags.name 上有 UNIQUE 约束，撞名的 sqlite 原文不适合直接甩给他看
    toast.error(`${action}失败：` + (/UNIQUE/i.test(msg) ? `${noun}名已存在` : msg));
  }

  async function create() {
    if (!draft.name.trim()) return;
    busy = true;
    try {
      await API.post(endpoint, { ...defaults, name: draft.name.trim(), color: draft.color });
      draft = { name: '', color: draft.color };
      await onchange();
    } catch (err) {
      warn(err, '添加');
    } finally {
      busy = false;
    }
  }

  function startEdit(t: Term) {
    editingId = t.id;
    editForm = { name: t.name, color: t.color };
  }

  async function save() {
    if (!editing) return;
    if (!editForm.name.trim()) {
      toast.error(`${noun}名不能为空`);
      return;
    }
    busy = true;
    try {
      await API.put(`${endpoint}/${editing.id}`, {
        ...defaults,
        ...editing,
        name: editForm.name.trim(),
        color: editForm.color
      });
      editingId = 0;
      await onchange();
    } catch (err) {
      warn(err, '保存');
    } finally {
      busy = false;
    }
  }

  async function remove(t: Term) {
    if (
      !(await ask({
        title: `删除${noun}「${t.name}」？`,
        detail: '引用它的记录会失去这个标记，删除后无法恢复。',
        danger: true,
        confirmLabel: '删除'
      }))
    )
      return;
    busy = true;
    try {
      await API.delete(`${endpoint}/${t.id}`);
      if (editingId === t.id) editingId = 0;
      await onchange();
      toast.ok('已删除');
    } catch (err) {
      warn(err, '删除');
    } finally {
      busy = false;
    }
  }
</script>

<ul class={pill ? 'mb-3 flex flex-wrap gap-2' : 'mb-3 space-y-1.5'}>
  {#each items as t (t.id)}
    {#if pill}
      <li class="term-pill" style={`--c: ${t.color};`}>
        <span class="q-dot" style={`--c: ${t.color};`}></span>
        <span class="max-w-40 truncate">{t.name}</span>
        <span class="ml-0.5 flex items-center">
          <button
            class="term-ico"
            title="编辑"
            aria-label={`编辑${noun}`}
            onclick={() => startEdit(t)}
          >
            <Pencil size={11} />
          </button>
          <button
            class="term-ico"
            title="删除"
            aria-label={`删除${noun}`}
            onclick={() => remove(t)}
          >
            <Trash2 size={11} />
          </button>
        </span>
      </li>
    {:else}
      <li
        class="flex items-center gap-2.5 rounded-[var(--q-radius-sm)] px-3 py-2 text-sm"
        style="background: var(--q-surface-2); border: 1px solid var(--q-border);"
      >
        <span class="q-dot shrink-0" style={`--c: ${t.color};`}></span>
        <span class="truncate">{t.name}</span>
        <span class="ml-auto flex shrink-0 items-center gap-0.5">
          <button
            class="q-iconbtn !h-7 !w-7"
            title="编辑"
            aria-label={`编辑${noun}`}
            onclick={() => startEdit(t)}
          >
            <Pencil size={14} />
          </button>
          <button
            class="q-iconbtn !h-7 !w-7"
            title="删除"
            aria-label={`删除${noun}`}
            onclick={() => remove(t)}
          >
            <Trash2 size={14} />
          </button>
        </span>
      </li>
    {/if}
  {:else}
    <li class="q-hint !mt-0">还没有{noun}</li>
  {/each}
</ul>

<div class="flex flex-wrap items-center gap-2">
  {#if editing}
    <input bind:value={editForm.name} class="q-input min-w-32 flex-1" placeholder="{noun}名" />
    <input type="color" bind:value={editForm.color} class="term-color" aria-label="选择颜色" />
    <button class="q-btn q-btn--primary" disabled={busy} onclick={save}>保存</button>
    <button
      class="q-iconbtn q-iconbtn--border"
      title="取消"
      aria-label="取消"
      onclick={() => (editingId = 0)}><X size={15} /></button
    >
  {:else}
    <input
      bind:value={draft.name}
      class="q-input min-w-32 flex-1"
      {placeholder}
      onkeydown={(e) => e.key === 'Enter' && create()}
    />
    <input type="color" bind:value={draft.color} class="term-color" aria-label="选择颜色" />
    <button class="q-btn q-btn--primary" disabled={busy} onclick={create}>
      <Plus size={14} /> 添加
    </button>
  {/if}
</div>

<style>
  /* 带分类色的胶囊：颜色由 --c 传入，操作图标随字色、悬停微亮 */
  .term-pill {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.26rem 0.38rem 0.26rem 0.62rem;
    border-radius: 999px;
    font-size: 0.74rem;
    line-height: 1.4;
    background: color-mix(in srgb, var(--c) 12%, var(--q-surface));
    color: var(--c);
    border: 1px solid color-mix(in srgb, var(--c) 32%, var(--q-border));
  }
  .term-ico {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.35rem;
    height: 1.35rem;
    border-radius: 999px;
    border: 0;
    background: transparent;
    color: inherit;
    cursor: pointer;
    opacity: 0.72;
    transition:
      background-color 0.15s ease,
      opacity 0.15s ease;
  }
  .term-ico:hover {
    opacity: 1;
    background: color-mix(in srgb, currentColor 16%, transparent);
  }

  /* 取色器与 q-input 等高对齐 */
  .term-color {
    width: 2.6rem;
    height: 2.3rem;
    flex: none;
    padding: 2px;
    cursor: pointer;
    background: var(--q-surface);
    border: 1px solid var(--q-border-strong);
    border-radius: var(--q-radius-sm);
  }
</style>
