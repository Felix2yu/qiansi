<script lang="ts">
  import { onMount } from 'svelte';
  import { route, navigate, applyStartPage } from './lib/router';
  import { API, SEARCH_LABEL, type SearchResult } from './lib/api';
  import {
    initTheme,
    theme,
    setThemeMode,
    setMoneyMask,
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
    Eye,
    EyeOff,
    Trash2,
    HeartPulse,
    Cake
  } from '@lucide/svelte';
  import Toaster from './lib/Toaster.svelte';
  import AskHost from './lib/AskHost.svelte';
  import { toast } from './lib/toast.svelte';
  import Today from './pages/Today.svelte';

  // 启动页：本地偏好在首帧渲染前把 / 换成用户指定的落点（无偏好则留在今日）
  applyStartPage(theme.startPage);

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

  // 侧栏导航：按使用情境分四组，桌面端与移动抽屉共用同一份定义
  const NAV_GROUPS = [
    {
      label: '纵览',
      items: [
        { label: '今日', path: '/', icon: Home },
        { label: '人物', path: '/people', icon: Users },
        { label: '往来', path: '/events', icon: CalendarDays },
        { label: '对话承诺', path: '/memos', icon: MessageCircle },
        { label: '金钱', path: '/money', icon: Wallet }
      ]
    },
    {
      label: '挂念',
      items: [
        { label: '纪念日', path: '/anniversaries', icon: Cake },
        { label: '待办', path: '/reminders', icon: Bell },
        { label: '渐远名单', path: '/drift', icon: HeartPulse }
      ]
    },
    {
      label: '洞察',
      items: [
        { label: '时间线', path: '/timeline', icon: History },
        { label: '关系图', path: '/graph', icon: Network },
        { label: '统计', path: '/analytics', icon: LineChart }
      ]
    },
    {
      label: '其他',
      items: [
        { label: '回收站', path: '/trash', icon: Trash2 },
        { label: '设置', path: '/settings', icon: Settings }
      ]
    }
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
    navigate(r.path);
  }

  function isActive(path: string) {
    const p = $route.path;
    if (path === '/') return p === '/' || p === '';
    return p === path || p.startsWith(path + '/');
  }

  // 导航点击沿用原有的 history 行为；桌面端与移动抽屉共用
  function go(path: string) {
    mobileOpen = false;
    if (location.pathname === path) return;
    history.pushState({}, '', path);
    window.dispatchEvent(new PopStateEvent('popstate'));
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

<div class="q-shell">
  <!-- 桌面侧栏 -->
  <aside class="q-side hidden md:flex">
    <div class="q-brand">
      <div class="q-brand__seal"><img src="/favicon.svg" alt="牵丝" /></div>
      <div>
        <div class="q-brand__name">牵丝</div>
        <div class="q-brand__sub">人情往来录</div>
      </div>
    </div>

    <div class="q-side__search">
      <Search size={14} class="q-side__searchicon" />
      <input
        bind:value={searchQ}
        oninput={runSearch}
        onkeydown={onSearchKey}
        placeholder="搜索人物、往来、对话…"
      />
      {#if searchOpen && searchResults.length > 0}
        <div class="q-searchpop">
          {#each searchResults as r, i (i)}
            <button onclick={() => openResult(r)}>
              <span class="q-badge shrink-0">{SEARCH_LABEL[r.type] || r.type}</span>
              <span class="truncate">{r.title}</span>
            </button>
          {/each}
        </div>
      {/if}
    </div>

    <nav class="q-side__nav">
      {#each NAV_GROUPS as g, gi (gi)}
        <div class="q-navgroup">
          <div class="q-navgroup__label">{g.label}</div>
          {#each g.items as it (it.path)}
            {@const Icon = it.icon}
            <a
              href={it.path}
              class="q-navitem"
              aria-current={isActive(it.path)}
              onclick={(e) => {
                e.preventDefault();
                go(it.path);
              }}
            >
              <Icon size={16} strokeWidth={1.9} />
              <span>{it.label}</span>
            </a>
          {/each}
        </div>
      {/each}
    </nav>

    <div class="q-side__foot">
      <span class="q-side__ver">v0.1 · local-only</span>
      <!-- 金额打码：投屏/截图场景一键遮住所有 ¥ 数字，再点恢复 -->
      <button
        class="q-iconbtn"
        onclick={() => setMoneyMask(!theme.maskMoney)}
        title={theme.maskMoney ? '显示金额' : '打码金额'}
        aria-label={theme.maskMoney ? '显示金额' : '打码金额'}
        aria-pressed={theme.maskMoney}
      >
        {#if theme.maskMoney}<EyeOff size={15} />{:else}<Eye size={15} />{/if}
      </button>
      <!-- 外观快速切换：跟随系统 / 浅色 / 深色，图标表示当前选择 -->
      <div class="q-seg q-seg--fill">
        {#each THEME_ICONS as t, i (i)}
          <button
            onclick={() => setThemeMode(t.mode)}
            title={THEME_LABEL[t.mode]}
            aria-label={THEME_LABEL[t.mode]}
            aria-pressed={theme.mode === t.mode}
            class="q-seg__btn !px-2"
          >
            <t.icon size={13} />
          </button>
        {/each}
      </div>
    </div>
  </aside>

  <!-- 移动顶栏 -->
  <div class="q-topbar md:hidden">
    <span class="q-topbar__name">牵丝</span>
    <div class="flex items-center gap-0.5">
      <!-- 移动端唯一的查询入口：手机才是「当场想起一个人」的终端，搜索不能只活在侧栏 -->
      <button
        class="q-iconbtn"
        onclick={() => {
          searchQ = '';
          searchResults = [];
          searchOpen = false;
          mobileSearch = true;
        }}
        title="搜索"
        aria-label="搜索"
      >
        <Search size={18} />
      </button>
      <!-- 移动端逐档循环切换外观：系统 → 浅色 → 深色 → 系统 -->
      <button
        class="q-iconbtn"
        onclick={() => setMoneyMask(!theme.maskMoney)}
        title={theme.maskMoney ? '显示金额' : '打码金额'}
        aria-label={theme.maskMoney ? '显示金额' : '打码金额'}
      >
        {#if theme.maskMoney}<EyeOff size={18} />{:else}<Eye size={18} />{/if}
      </button>
      <button
        class="q-iconbtn"
        onclick={() => setThemeMode(nextMode(theme.mode))}
        title={`外观：${THEME_LABEL[theme.mode]}`}
        aria-label={`外观：${THEME_LABEL[theme.mode]}，点击切换`}
      >
        {#if theme.mode === 'system'}<Monitor size={18} />{:else if theme.mode === 'light'}<Sun
            size={18}
          />{:else}<Moon size={18} />{/if}
      </button>
      <button class="q-iconbtn" onclick={() => (mobileOpen = !mobileOpen)} aria-label="菜单">
        {#if mobileOpen}<X size={22} />{:else}<Menu size={22} />{/if}
      </button>
    </div>
  </div>

  <!-- 移动导航抽屉：与侧栏共用 NAV_GROUPS -->
  {#if mobileOpen}
    <div class="q-sheet md:hidden">
      {#each NAV_GROUPS as g, gi (gi)}
        <div class="q-navgroup">
          <div class="q-navgroup__label">{g.label}</div>
          {#each g.items as it (it.path)}
            {@const Icon = it.icon}
            <a
              href={it.path}
              class="q-navitem"
              aria-current={isActive(it.path)}
              onclick={(e) => {
                e.preventDefault();
                go(it.path);
              }}
            >
              <Icon size={17} strokeWidth={1.9} />
              <span>{it.label}</span>
            </a>
          {/each}
        </div>
      {/each}
    </div>
  {/if}

  <!-- 移动端全屏搜索层：与侧栏搜索共用同一份 query 与结果，不另起一套逻辑 -->
  {#if mobileSearch}
    <div class="fixed inset-0 z-40 flex flex-col md:hidden" style="background: var(--q-bg);">
      <div
        class="flex items-center gap-2 px-3"
        style="height: 3.4rem; background: var(--q-surface); border-bottom: 1px solid var(--q-border);"
      >
        <input
          use:autofocus
          bind:value={searchQ}
          oninput={runSearch}
          onkeydown={onSearchKey}
          placeholder="搜索人物、往来、对话…"
          class="q-input !py-2"
        />
        <button
          class="q-btn q-btn--subtle q-btn--sm shrink-0"
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
          <div class="py-12 text-center text-sm" style="color: var(--q-muted);">
            {searchQ.trim() ? '没有匹配的记录' : '输入名字、事由或对话内容'}
          </div>
        {:else}
          <div class="p-2">
            {#each searchResults as r, i (i)}
              <button
                class="flex w-full items-center gap-2 rounded-lg px-3 py-3 text-left"
                style="color: var(--q-text);"
                onclick={() => openResult(r)}
              >
                <span class="q-badge shrink-0">{SEARCH_LABEL[r.type] || r.type}</span>
                <span class="truncate">{r.title}</span>
              </button>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  {/if}

  <!-- 页面区：每个页面自带 .q-page 容器（统一宽度与留白） -->
  <main class="q-main">
    {#if cur === 'Today'}
      <Today />
    {:else if typeof cur === 'string'}
      {#if pages[cur]}
        {@const Page = pages[cur]}
        {#if cur === 'PeopleNew'}<Page mode="new" />
        {:else if cur === 'Timeline'}<Page onlyTimeline={true} />
        {:else}<Page />{/if}
      {:else}
        <div class="q-page text-center" style="color: var(--q-muted);">加载中…</div>
      {/if}
    {:else if pages.PersonDetail}
      {@const Page = pages.PersonDetail}
      <Page id={cur.id} />
    {:else}
      <div class="q-page text-center" style="color: var(--q-muted);">加载中…</div>
    {/if}
  </main>
</div>

<!-- 反馈条与确认框：全站一套，挂在最外层 -->
<Toaster />
<AskHost />
