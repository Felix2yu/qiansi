<script lang="ts">
  import { onDestroy } from 'svelte';
  import { API, yuan, type HuozhiSearchResult, type HuozhiBill } from './api';
  import { toast } from './toast.svelte';
  import { Search, X, ExternalLink, RefreshCw } from '@lucide/svelte';

  // 从货殖挑账单挂到这次往来上。搜索在服务端做（密钥只留在后端），
  // 这里只负责「输入 → 展示 → 勾选 → 交回 id」，不缓存账单本体。
  let {
    linkedIds = [],
    onclose,
    onpick
  }: {
    /** 已经关联过的远端 id，列表里置灰，避免重复勾选 */
    linkedIds?: string[];
    onclose: () => void;
    onpick: (ids: string[]) => void;
  } = $props();

  let q = $state('');
  let from = $state('');
  let to = $state('');
  let items = $state<HuozhiBill[]>([]);
  let base = $state('');
  let total = $state(0);
  let page = $state(1);
  let loading = $state(false);
  let failed = $state('');
  let picked = $state<string[]>([]);
  let timer: ReturnType<typeof setTimeout> | undefined;

  const linked = $derived(new Set(linkedIds));

  async function run(reset = true) {
    if (reset) page = 1;
    loading = true;
    failed = '';
    try {
      // 只列支出：往来关联的是「花了多少钱」，把收入/余额调整混进来只会让人选错
      const qs = new URLSearchParams({
        q: q.trim(),
        type: 'expense',
        page: String(page),
        page_size: '20'
      });
      if (from) qs.set('from', from);
      if (to) qs.set('to', to);
      const res = await API.get<HuozhiSearchResult>(`/api/v1/huozhi/bills?${qs}`);
      items = res.items || [];
      base = res.base || '';
      total = res.total || 0;
    } catch (err: any) {
      failed = err?.message || '搜索失败';
      items = [];
    } finally {
      loading = false;
    }
  }

  function schedule() {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => void run(true), 250);
  }

  function toggle(b: HuozhiBill) {
    const id = String(b.id);
    if (linked.has(id)) return;
    picked = picked.includes(id) ? picked.filter((x) => x !== id) : [...picked, id];
  }

  function billUrl(id: string) {
    // 货殖的账单页是登录态路由，直接开新窗口，不在这里内嵌 iframe
    return base ? `${base}/transactions/edit/${id}` : '';
  }

  onDestroy(() => {
    if (timer) clearTimeout(timer);
  });

  void run(true);
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key === 'Escape') onclose();
  }}
/>

<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
  <button
    type="button"
    aria-label="关闭"
    class="absolute inset-0 cursor-default"
    style="background: rgba(0,0,0,.35); border: 0;"
    onclick={onclose}
  ></button>
  <div
    class="relative flex max-h-[85vh] w-full max-w-xl flex-col overflow-hidden rounded-2xl"
    style="background: var(--q-surface); border: 1px solid var(--q-border);"
  >
    <div class="flex items-center justify-between px-4 py-3">
      <h2 class="text-sm font-semibold">关联货殖账单</h2>
      <button class="rounded p-1" style="color: var(--q-muted);" onclick={onclose} aria-label="关闭"
        ><X size={16} /></button
      >
    </div>

    <div class="flex flex-wrap items-center gap-2 px-4 pb-3">
      <div class="relative min-w-[160px] flex-1">
        <Search
          size={13}
          class="absolute top-1/2 left-2 -translate-y-1/2"
          style="color: var(--q-muted);"
        />
        <input
          bind:value={q}
          oninput={schedule}
          placeholder="搜索备注 / 商家 / 地点"
          class="w-full rounded-lg py-1.5 pr-2 pl-7 text-sm outline-none"
          style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);"
        />
      </div>
      <input
        type="date"
        bind:value={from}
        onchange={() => run(true)}
        aria-label="起始日期"
        class="rounded-lg px-2 py-1.5 text-xs outline-none"
        style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);"
      />
      <span class="text-xs" style="color: var(--q-muted);">至</span>
      <input
        type="date"
        bind:value={to}
        onchange={() => run(true)}
        aria-label="截止日期"
        class="rounded-lg px-2 py-1.5 text-xs outline-none"
        style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);"
      />
      <button
        type="button"
        class="rounded-lg p-1.5"
        style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-muted);"
        title="重新搜索"
        onclick={() => run(true)}><RefreshCw size={13} /></button
      >
    </div>

    <div
      class="min-h-[120px] flex-1 overflow-y-auto"
      style="border-top: 1px solid var(--q-border);"
    >
      {#if loading && items.length === 0}
        <p class="px-4 py-6 text-center text-xs" style="color: var(--q-muted);">查询中…</p>
      {:else if failed}
        <p class="px-4 py-6 text-center text-xs" style="color: var(--q-danger);">{failed}</p>
      {:else if items.length === 0}
        <p class="px-4 py-6 text-center text-xs" style="color: var(--q-muted);">
          货殖里没有符合条件的账单
        </p>
      {:else}
        {#each items as b (b.id)}
          {@const id = String(b.id)}
          {@const done = linked.has(id)}
          {@const on = picked.includes(id)}
          <button
            type="button"
            class="flex w-full items-center gap-2.5 px-4 py-2 text-left"
            style={done ? 'opacity:.45; cursor:not-allowed;' : on ? 'background: var(--q-bg);' : ''}
            onclick={() => toggle(b)}
            aria-pressed={on}
          >
            <span
              class="flex h-4 w-4 shrink-0 items-center justify-center rounded"
              style="border: 1px solid {on ? 'var(--q-theme)' : 'var(--q-border)'}; background: {on
                ? 'var(--q-theme)'
                : 'transparent'}; color: #fff; font-size: 11px;"
            >
              {on ? '✓' : ''}
            </span>
            <span class="min-w-0 flex-1">
              <span class="flex items-baseline justify-between gap-2">
                <span class="truncate text-sm">{b.description || '（无备注）'}</span>
                <span class="shrink-0 text-sm tabular-nums">¥{yuan(b.amount_fen)}</span>
              </span>
              <span class="mt-0.5 flex items-center gap-1.5 text-xs" style="color: var(--q-muted);">
                <span>{b.tx_date}</span>
                {#if b.category_name}<span>· {b.category_name}</span>{/if}
                {#if b.account_name}<span>· {b.account_name}</span>{/if}
                {#if done}<span>· 已关联</span>{/if}
              </span>
            </span>
            {#if base}
              <a
                href={billUrl(id)}
                target="_blank"
                rel="noreferrer"
                class="shrink-0 rounded p-1"
                style="color: var(--q-muted);"
                title="在货殖里打开"
                onclick={(e) => e.stopPropagation()}><ExternalLink size={13} /></a
              >
            {/if}
          </button>
        {/each}
      {/if}
    </div>

    <div class="flex items-center justify-between gap-2 px-4 py-3">
      <span class="text-xs" style="color: var(--q-muted);">
        共 {total} 张{#if total > page * 20}
          · 只显示前 {page * 20} 张，用关键词缩小范围{/if}
      </span>
      <div class="flex items-center gap-2">
        <button
          type="button"
          class="rounded-lg px-3 py-1.5 text-sm"
          style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-muted);"
          onclick={onclose}>取消</button
        >
        <button
          type="button"
          class="rounded-lg px-3 py-1.5 text-sm"
          style="background: var(--q-theme); color: #fff;"
          disabled={picked.length === 0}
          onclick={() => {
            if (picked.length === 0) {
              toast.error('先勾选要关联的账单');
              return;
            }
            onpick(picked);
          }}>关联所选（{picked.length}）</button
        >
      </div>
    </div>
  </div>
</div>
