<script lang="ts">
  import { onDestroy } from 'svelte';
  import { selfFirst, personLabel, loadSelf } from './self.svelte';
  import { searchPeople, resolvePerson, cachedPerson, type PersonLite } from './personSearch';
  import { X, Search, ChevronDown } from '@lucide/svelte';

  // 选人控件：输入即搜，不再依赖「先把 500 人全拉下来」的名单。
  // 展开时原地换成搜索框，免得在弹窗里被 overflow 裁掉。
  let {
    value = $bindable(''),
    selectedName = '',
    placeholder = '搜索联系人',
    clearable = true,
    exclude = [],
    prefer = [],
    compact = false,
    onchange
  }: {
    value?: string;
    /** 调用方已知姓名时直接传进来，省一次回查 */
    selectedName?: string;
    placeholder?: string;
    /** 必选的场景（关系两端、待办归属）不给清除按钮 */
    clearable?: boolean;
    exclude?: string[];
    /**
     * 优先展示的人（如本往来的参与人）。不输入关键词时只列这一组，
     * 免得「这笔钱花在谁身上」在满屏联系人里翻；真要挂给没参与的人，打字就能搜到。
     */
    prefer?: PersonLite[];
    /** 行内/窄栏用的小号样式 */
    compact?: boolean;
    onchange?: (picked: PersonLite | null) => void;
  } = $props();

  let open = $state(false);
  // 显示名要能标出「我 · xxx」；settings 只拉一次，多个实例共用同一趟请求
  void loadSelf();
  let query = $state('');
  let results = $state<PersonLite[]>([]);
  let selected = $state<PersonLite | null>(null);
  let searching = $state(false);
  let failed = $state(false);
  let active = $state(0);
  let timer: ReturnType<typeof setTimeout> | undefined;
  let wrap: HTMLDivElement | undefined = $state();
  let input: HTMLInputElement | undefined = $state();

  function matchText(p: PersonLite, q: string) {
    if (!q) return true;
    const s = q.toLowerCase();
    return p.name.toLowerCase().includes(s) || (p.nickname || '').toLowerCase().includes(s);
  }

  const typed = $derived(query.trim());
  // 两组：先参与人（prefer），再全局搜索结果（只在输入关键词后才出现）
  const preferList = $derived(
    prefer.filter((p) => p.id && !exclude.includes(p.id) && matchText(p, typed))
  );
  const rest = $derived(prefer.length > 0 && !typed ? [] : results);
  const total = $derived(preferList.length + rest.length);

  // 外部改了 value（回填表单、清空筛选）时跟着换显示名
  $effect(() => {
    const v = value;
    if (!v) {
      if (selected) selected = null;
      return;
    }
    if (selected?.id === v) return;
    if (selectedName) {
      selected = { id: v, name: selectedName };
      return;
    }
    const hit = cachedPerson(v);
    if (hit) {
      selected = hit;
      return;
    }
    void resolvePerson(v).then((p) => {
      if (p && value === p.id) selected = p;
    });
  });

  function schedule() {
    if (timer) clearTimeout(timer);
    timer = setTimeout(run, 200);
  }

  async function run() {
    searching = true;
    failed = false;
    try {
      // 本人排最前是这个控件的承诺，不能指望宿主页先拉过 settings
      await loadSelf();
      const list = await searchPeople(query.trim(), [...exclude, ...prefer.map((p) => p.id)]);
      results = selfFirst(list);
      active = 0;
    } catch {
      failed = true;
      results = [];
    } finally {
      searching = false;
    }
  }

  async function show() {
    open = true;
    // 空关键词也先给一屏常用联系人，点一下就能选，不必逼用户猜名字
    await run();
    input?.focus();
  }

  function hide() {
    open = false;
    query = '';
  }

  function pick(p: PersonLite) {
    selected = p;
    value = p.id;
    onchange?.(p);
    hide();
  }

  function clear(e: MouseEvent) {
    e.stopPropagation();
    selected = null;
    value = '';
    onchange?.(null);
  }

  function personAt(i: number): PersonLite | undefined {
    return i < preferList.length ? preferList[i] : rest[i - preferList.length];
  }

  function onKeydown(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === 'Escape') {
      hide();
      return;
    }
    if (e.key === 'ArrowDown') {
      active = Math.min(active + 1, Math.max(total - 1, 0));
      e.preventDefault();
      return;
    }
    if (e.key === 'ArrowUp') {
      active = Math.max(active - 1, 0);
      e.preventDefault();
      return;
    }
    if (e.key === 'Enter') {
      const p = personAt(active);
      if (p) {
        pick(p);
        e.preventDefault();
      }
      return;
    }
  }

  function onDocClick(e: MouseEvent) {
    if (open && wrap && !wrap.contains(e.target as Node)) hide();
  }
  document.addEventListener('mousedown', onDocClick);
  onDestroy(() => {
    document.removeEventListener('mousedown', onDocClick);
    if (timer) clearTimeout(timer);
  });
</script>

<svelte:window onkeydown={onKeydown} />

<div class="relative" bind:this={wrap}>
  {#if !open}
    <div class="flex items-center gap-1">
      <button
        type="button"
        class={compact ? 'q-input !py-1.5 text-xs' : 'q-input'}
        class:text-left={true}
        style="cursor: pointer;"
        onclick={show}
        aria-label={selected ? personLabel(selected) : placeholder}
      >
        <span class="flex items-center justify-between gap-2">
          <span class={selected ? 'truncate' : 'truncate !text-[var(--q-faint)]'}>
            {selected ? personLabel(selected) : placeholder}
          </span>
          <ChevronDown size={compact ? 12 : 14} class="shrink-0 opacity-60" />
        </span>
      </button>
      {#if clearable && value}
        <button type="button" class="q-iconbtn !h-7 !w-7 shrink-0" title="清除" onclick={clear}
          ><X size={compact ? 12 : 14} /></button
        >
      {/if}
    </div>
  {:else}
    <div
      class="absolute top-0 right-0 left-0 z-40 overflow-hidden rounded-[var(--q-radius-sm)]"
      style="background: var(--q-surface); border: 1px solid var(--q-border-strong); box-shadow: var(--q-shadow-pop);"
      data-esc-owner
    >
      <div class="flex items-center gap-1.5 px-2.5 py-1.5">
        <Search size={compact ? 12 : 14} class="shrink-0" style="color: var(--q-faint);" />
        <input
          bind:this={input}
          bind:value={query}
          oninput={schedule}
          class="min-w-0 flex-1 py-1 text-sm outline-none"
          style="background: transparent; color: var(--q-text); border: 0;"
          {placeholder}
          aria-label="搜索联系人"
        />
        <button type="button" class="q-iconbtn !h-6 !w-6" title="收起" onclick={hide}
          ><X size={13} /></button
        >
      </div>
      <div class="max-h-56 overflow-y-auto" style="border-top: 1px solid var(--q-border);">
        {#if preferList.length > 0}
          <p class="px-3 pt-2 pb-0.5 text-xs tracking-wide" style="color: var(--q-faint);">
            参与人
          </p>
          {#each preferList as p, i (p.id)}
            <button
              type="button"
              class="block w-full px-3 py-2 text-left text-sm"
              style={i === active ? 'background: var(--q-surface-2);' : 'color: var(--q-text);'}
              onmouseenter={() => (active = i)}
              onclick={() => pick(p)}
            >
              {personLabel(p)}
              {#if p.nickname && p.nickname !== p.name}<span
                  class="ml-1 text-xs"
                  style="color: var(--q-muted);">{p.nickname}</span
                >{/if}
            </button>
          {/each}
        {/if}
        {#if rest.length > 0 && prefer.length > 0}
          <p
            class="px-3 pt-2 pb-0.5 text-xs tracking-wide"
            style="color: var(--q-faint); border-top: 1px solid var(--q-border);"
          >
            其他联系人
          </p>
        {/if}
        {#each rest as p, j (p.id)}
          <button
            type="button"
            class="block w-full px-3 py-2 text-left text-sm"
            style={preferList.length + j === active
              ? 'background: var(--q-surface-2);'
              : 'color: var(--q-text);'}
            onmouseenter={() => (active = preferList.length + j)}
            onclick={() => pick(p)}
          >
            {personLabel(p)}
            {#if p.nickname && p.nickname !== p.name}<span
                class="ml-1 text-xs"
                style="color: var(--q-muted);">{p.nickname}</span
              >{/if}
          </button>
        {/each}
        {#if total === 0}
          {#if searching && typed}
            <p class="px-3 py-2 text-xs" style="color: var(--q-muted);">搜索中…</p>
          {:else if failed}
            <p class="px-3 py-2 text-xs" style="color: var(--q-danger);">搜索失败，请重试</p>
          {:else if prefer.length > 0}
            <p class="px-3 py-2 text-xs" style="color: var(--q-muted);">
              没有匹配的联系人 · 参与人里也没有
            </p>
          {:else}
            <p class="px-3 py-2 text-xs" style="color: var(--q-muted);">
              没有匹配的人 · 换个关键词，或先到通讯录里新建
            </p>
          {/if}
        {/if}
      </div>
    </div>
  {/if}
</div>
