<script lang="ts">
  import { onMount } from 'svelte';
  import { route, navigate } from './lib/router';
  import { API, SEARCH_LABEL, type SearchResult } from './lib/api';
  import {
    initTheme,
    theme,
    setThemeMode,
    nextMode,
    THEME_LABEL,
    type ThemeMode
  } from './lib/theme.svelte';
  import type { Component } from 'svelte';
  import {
    Home,
    Users,
    CalendarDays,
    MessageCircle,
    Wallet,
    Bell,
    LineChart,
    History,
    Network,
    Settings,
    Menu,
    X,
    Search,
    Monitor,
    Sun,
    Moon,
    Trash2,
    HeartPulse
  } from '@lucide/svelte';
  import Toaster from './lib/Toaster.svelte';
  import AskHost from './lib/AskHost.svelte';
  import { toast } from './lib/toast.svelte';
  import Today from './pages/Today.svelte';

  // 只有首页静态引入：其余页面各自成块，用到才下载（关系图/统计带 echarts，最重）
  const PAGES: Record<string, () => Promise<{ default: Component<any> }>> = {
    People: () => import('./pages/People.svelte'),
    PeopleNew: () => import('./pages/People.svelte'),
    PersonDetail: () => import('./pages/PersonDetail.svelte'),
    Events: () => import('./pages/Events.svelte'),
    Timeline: () => import('./pages/Events.svelte'),
    Memos: () => import('./pages/Memos.svelte'),
    Money: () => import('./pages/Money.svelte'),
    Anniversaries: () => import('./pages/Anniversaries.svelte'),
    Reminders: () => import('./pages/Reminders.svelte'),
    Drift: () => import('./pages/Drift.svelte'),
    Graph: () => import('./pages/Graph.svelte'),
    Analytics: () => import('./pages/Analytics.svelte'),
    Trash: () => import('./pages/Trash.svelte'),
    Settings: () => import('./pages/Settings.svelte')
  };

  const navItems = [
    { label: '今日', path: '/', icon: Home },
    { label: '人物', path: '/people', icon: Users },
    { label: '往来', path: '/events', icon: CalendarDays },
    { label: '对话/承诺', path: '/memos', icon: MessageCircle },
    { label: '金钱', path: '/money', icon: Wallet },
    { label: '纪念日', path: '/anniversaries', icon: CalendarDays },
    { label: '待办', path: '/reminders', icon: Bell },
    { label: '渐远名单', path: '/drift', icon: HeartPulse },
    { label: '时间线', path: '/timeline', icon: History },
    { label: '关系图', path: '/graph', icon: Network },
    { label: '统计', path: '/analytics', icon: LineChart },
    { label: '回收站', path: '/trash', icon: Trash2 },
    { label: '设置', path: '/settings', icon: Settings }
  ];

  // 侧边栏外观切换的三档，顺序即"从自动到最具体"
  const THEME_ICONS: { mode: ThemeMode; icon: typeof Monitor }[] = [
    { mode: 'system', icon: Monitor },
    { mode: 'light', icon: Sun },
    { mode: 'dark', icon: Moon }
  ];

  let mobileOpen = $state(false);
  let mobileSearch = $state(false);

  // 全屏搜索层弹出时把光标放进输入框：手机上多一次点击就多一次放弃
  function autofocus(el: HTMLInputElement) {
    el.focus();
  }

  // 主题：本地值同步应用（防首屏闪白），再从服务端同步偏好。
  // 判定逻辑集中在 lib/theme.svelte.ts，这里只负责触发。
  onMount(() => {
    initTheme();
    // 头像与导出链接带不了 Authorization 头，先拿令牌换一枚只读会话 cookie
    API.ensureSession();
    // 兜底：漏了 catch 的写操作至少要在屏幕上留个痕。「点了没反应」是静默失效最难查的形态。
    window.addEventListener('unhandledrejection', (e) => {
      const msg = (e.reason as any)?.message || (typeof e.reason === 'string' ? e.reason : '');
      if (msg) toast.error('操作失败：' + msg);
    });
  });

  // 全局搜索：输入即查，回车跳到第一个结果
  let searchQ = $state('');
  let searchResults = $state<SearchResult[]>([]);
  let searchOpen = $state(false);
  let searchTimer: ReturnType<typeof setTimeout> | undefined;

  function runSearch() {
    clearTimeout(searchTimer);
    const q = searchQ.trim();
    if (!q) {
      searchResults = [];
      searchOpen = false;
      return;
    }
    searchTimer = setTimeout(async () => {
      try {
        searchResults = (await API.get(
          `/api/v1/search?q=${encodeURIComponent(q)}&limit=6`
        )) as SearchResult[];
        searchOpen = true;
      } catch {
        searchResults = [];
      }
    }, 200);
  }

  function onSearchKey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      searchOpen = false;
      mobileSearch = false;
      return;
    }
    if (e.key === 'Enter' && searchResults.length > 0) openResult(searchResults[0]);
  }

  function openResult(r: SearchResult) {
    searchOpen = false;
    mobileSearch = false;
    navigate(r.type === 'person' ? r.path : r.path);
  }

  function isActive(path: string) {
    const p = $route.path;
    if (path === '/') return p === '/' || p === '';
    return p === path || p.startsWith(path + '/');
  }

  function current() {
    const p = $route.path;
    if (p === '/' || p === '') return 'Today';
    if (p === '/people/new') return 'PeopleNew';
    if (p.startsWith('/people/')) {
      const id = p.split('/').pop()!;
      return { name: 'PersonDetail', id };
    }
    const map: Record<string, string> = {
      '/people': 'People',
      '/events': 'Events',
      '/memos': 'Memos',
      '/money': 'Money',
      '/anniversaries': 'Anniversaries',
      '/reminders': 'Reminders',
      '/timeline': 'Timeline',
      '/graph': 'Graph',
      '/analytics': 'Analytics',
      '/trash': 'Trash',
      '/settings': 'Settings',
      '/drift': 'Drift'
    };
    return map[p] || 'Today';
  }

  const cur = $derived(current());
  const pageKey = $derived(typeof cur === 'string' ? cur : 'PersonDetail');
  const pages = $state<Record<string, Component<any> | null>>({});

  $effect(() => {
    const key = pageKey;
    if (pages[key] || !PAGES[key]) return;
    PAGES[key]().then((m) => {
      pages[key] = m.default;
    });
  });
</script>

<div class="flex min-h-screen">
  <!-- sidebar -->
  <aside
    class="hidden w-60 shrink-0 flex-col border-r md:flex"
    style="background: var(--q-surface); border-color: var(--q-border);"
  >
    <div class="flex items-center gap-2 border-b px-5 py-5" style="border-color: var(--q-border);">
      <div
        class="flex h-8 w-8 items-center justify-center rounded-xl font-semibold text-white"
        style="background: var(--q-theme);"
      >
        牵
      </div>
      <div>
        <div class="leading-none font-semibold">牵丝</div>
        <div class="text-xs" style="color: var(--q-muted);">人际关系记录</div>
      </div>
    </div>
    <div class="px-3 pt-3">
      <div class="relative">
        <Search
          size={14}
          class="absolute top-1/2 left-3 -translate-y-1/2"
          style="color: var(--q-muted);"
        />
        <input
          bind:value={searchQ}
          oninput={runSearch}
          onkeydown={onSearchKey}
          placeholder="搜索人物、往来、对话…"
          class="w-full rounded-lg py-2 pr-3 pl-9 text-sm outline-none"
          style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);"
        />
      </div>
      {#if searchOpen && searchResults.length > 0}
        <ul
          class="mt-1 overflow-hidden rounded-lg text-sm"
          style="background: var(--q-surface); border: 1px solid var(--q-border);"
        >
          {#each searchResults as r, i (i)}
            <li>
              <button
                class="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-black/5 dark:hover:bg-white/5"
                onclick={() => openResult(r)}
              >
                <span
                  class="shrink-0 rounded px-1.5 py-0.5 text-[10px]"
                  style="background: var(--q-bg); color: var(--q-muted);"
                  >{SEARCH_LABEL[r.type] || r.type}</span
                >
                <span class="truncate">{r.title}</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
    <nav class="flex-1 py-3">
      {#each navItems as it, i (i)}
        {@const Icon = it.icon}
        <a
          href={it.path}
          class="flex items-center gap-3 px-5 py-2 text-sm transition-colors"
          style="color: var(--q-text); {isActive(it.path)
            ? 'background: color-mix(in srgb, var(--q-theme) 12%, transparent);'
            : ''}"
          onclick={(e) => {
            e.preventDefault();
            if (location.pathname === it.path) return;
            history.pushState({}, '', it.path);
            window.dispatchEvent(new PopStateEvent('popstate'));
          }}
        >
          <Icon size={18} />
          <span>{it.label}</span>
        </a>
      {/each}
    </nav>
    <div class="flex items-center gap-1.5 px-5 py-3">
      <div class="mr-auto text-xs" style="color: var(--q-muted);">v0.1 · local-only</div>
      <!-- 外观快速切换：跟随系统 / 浅色 / 深色，图标表示当前选择 -->
      <div
        class="flex items-center gap-0.5 rounded-lg p-0.5"
        style="background: var(--q-bg); border: 1px solid var(--q-border);"
      >
        {#each THEME_ICONS as t, i (i)}
          <button
            onclick={() => setThemeMode(t.mode)}
            title={THEME_LABEL[t.mode]}
            aria-label={THEME_LABEL[t.mode]}
            aria-pressed={theme.mode === t.mode}
            class="flex h-6 w-6 items-center justify-center rounded"
            style="color: {theme.mode === t.mode ? 'var(--q-theme)' : 'var(--q-muted)'};"
          >
            <t.icon size={13} />
          </button>
        {/each}
      </div>
    </div>
  </aside>

  <!-- mobile top bar -->
  <div
    class="fixed inset-x-0 top-0 z-30 flex items-center justify-between border-b px-4 py-2 md:hidden"
    style="background: var(--q-surface); border-color: var(--q-border);"
  >
    <div class="font-semibold">牵丝</div>
    <div class="flex items-center gap-1">
      <!-- 移动端唯一的查询入口：手机才是「当场想起一个人」的终端，搜索不能只活在侧栏 -->
      <button
        onclick={() => {
          searchQ = '';
          searchResults = [];
          searchOpen = false;
          mobileSearch = true;
        }}
        title="搜索"
        aria-label="搜索"
        class="flex h-7 w-7 items-center justify-center rounded"
        style="color: var(--q-muted);"
      >
        <Search size={17} />
      </button>
      <!-- 移动端逐档循环切换外观：系统 → 浅色 → 深色 → 系统 -->
      <button
        onclick={() => setThemeMode(nextMode(theme.mode))}
        title={`外观：${THEME_LABEL[theme.mode]}`}
        aria-label={`外观：${THEME_LABEL[theme.mode]}，点击切换`}
        class="flex h-7 w-7 items-center justify-center rounded"
        style="color: var(--q-muted);"
      >
        {#if theme.mode === 'system'}<Monitor size={17} />{:else if theme.mode === 'light'}<Sun
            size={17}
          />{:else}<Moon size={17} />{/if}
      </button>
      <button
        onclick={() => (mobileOpen = !mobileOpen)}
        class="rounded p-1 hover:bg-black/5 dark:hover:bg-white/5"
        aria-label="菜单"
      >
        {#if mobileOpen}<X size={22} />{:else}<Menu size={22} />{/if}
      </button>
    </div>
  </div>

  {#if mobileOpen}
    <div
      class="fixed inset-0 top-11 z-20 overflow-y-auto md:hidden"
      style="background: var(--q-surface);"
    >
      {#each navItems as it, i (i)}
        {@const Icon = it.icon}
        <a
          href={it.path}
          class="flex items-center gap-3 border-b px-5 py-3"
          style="border-color: var(--q-border); color: var(--q-text); {isActive(it.path)
            ? 'background: color-mix(in srgb, var(--q-theme) 12%, transparent);'
            : ''}"
          onclick={() => (mobileOpen = false)}
        >
          <Icon size={18} />
          <span>{it.label}</span>
        </a>
      {/each}
    </div>
  {/if}

  <!-- 移动端全屏搜索层：与侧栏搜索共用同一份 query 与结果，不另起一套逻辑 -->
  {#if mobileSearch}
    <div class="fixed inset-0 z-40 flex flex-col md:hidden" style="background: var(--q-bg);">
      <div
        class="flex items-center gap-2 border-b px-3 py-2"
        style="background: var(--q-surface); border-color: var(--q-border);"
      >
        <input
          use:autofocus
          bind:value={searchQ}
          oninput={runSearch}
          onkeydown={onSearchKey}
          placeholder="搜索人物、往来、对话…"
          class="min-w-0 flex-1 rounded-lg px-3 py-2 text-sm outline-none"
          style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);"
        />
        <button
          class="px-2 py-1 text-sm"
          style="color: var(--q-muted);"
          onclick={() => {
            mobileSearch = false;
            searchQ = '';
            searchResults = [];
            searchOpen = false;
          }}>取消</button
        >
      </div>
      <div class="flex-1 overflow-y-auto">
        {#if searchResults.length === 0}
          <div class="py-10 text-center text-sm" style="color: var(--q-muted);">
            {searchQ.trim() ? '没有匹配的记录' : '输入名字、事由或对话内容'}
          </div>
        {:else}
          <ul>
            {#each searchResults as r, i (i)}
              <li>
                <button
                  class="flex w-full items-center gap-2 border-b px-4 py-3 text-left"
                  style="border-color: var(--q-border); color: var(--q-text);"
                  onclick={() => openResult(r)}
                >
                  <span
                    class="shrink-0 rounded px-1.5 py-0.5 text-[10px]"
                    style="background: var(--q-surface); color: var(--q-muted);"
                    >{SEARCH_LABEL[r.type] || r.type}</span
                  >
                  <span class="truncate">{r.title}</span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </div>
    </div>
  {/if}

  <!-- main -->
  <main class="min-w-0 flex-1 pt-12 md:pt-0">
    <div class="mx-auto max-w-6xl px-4 py-6 md:px-8 md:py-8">
      {#if cur === 'Today'}
        <Today />
      {:else if typeof cur === 'string'}
        {#if pages[cur]}
          {@const Page = pages[cur]}
          {#if cur === 'PeopleNew'}<Page mode="new" />
          {:else if cur === 'Timeline'}<Page onlyTimeline={true} />
          {:else}<Page />{/if}
        {:else}
          <div class="py-16 text-center" style="color: var(--q-muted);">加载中…</div>
        {/if}
      {:else if pages.PersonDetail}
        {@const Page = pages.PersonDetail}
        <Page id={cur.id} />
      {:else}
        <div class="py-16 text-center" style="color: var(--q-muted);">加载中…</div>
      {/if}
    </div>
  </main>
</div>

<!-- 反馈条与确认框：全站一套，挂在最外层 -->
<Toaster />
<AskHost />

<style>
  a[href]:hover {
    background: color-mix(in srgb, var(--q-theme) 8%, transparent);
  }
</style>
