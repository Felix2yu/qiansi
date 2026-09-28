<script lang="ts">
  import { onMount } from 'svelte'
  import { route, navigate } from './lib/router'
  import { API, SEARCH_LABEL, type SearchResult } from './lib/api'
  import { Home, Users, CalendarDays, MessageCircle, Wallet, Bell, LineChart, History, Network, Settings, Menu, X, Search } from '@lucide/svelte'
  import Today from './pages/Today.svelte'
  import People from './pages/People.svelte'
  import PersonDetail from './pages/PersonDetail.svelte'
  import Events from './pages/Events.svelte'
  import Memos from './pages/Memos.svelte'
  import Money from './pages/Money.svelte'
  import Anniversaries from './pages/Anniversaries.svelte'
  import Reminders from './pages/Reminders.svelte'
  import Graph from './pages/Graph.svelte'
  import Analytics from './pages/Analytics.svelte'
  import SettingsPage from './pages/Settings.svelte'

  const navItems = [
    { label: '今日', path: '/', icon: Home },
    { label: '人物', path: '/people', icon: Users },
    { label: '往来', path: '/events', icon: CalendarDays },
    { label: '对话/承诺', path: '/memos', icon: MessageCircle },
    { label: '金钱', path: '/money', icon: Wallet },
    { label: '纪念日', path: '/anniversaries', icon: CalendarDays },
    { label: '待办', path: '/reminders', icon: Bell },
    { label: '时间线', path: '/timeline', icon: History },
    { label: '关系图', path: '/graph', icon: Network },
    { label: '统计', path: '/analytics', icon: LineChart },
    { label: '设置', path: '/settings', icon: Settings },
  ]

  let mobileOpen = $state(false)

  // 保存过的主题色与暗色模式要在刷新后继续生效
  onMount(() => {
    const saved = localStorage.getItem('q_theme')
    if (saved) document.documentElement.style.setProperty('--q-theme', saved)
    if (localStorage.getItem('q_dark') === '1') document.documentElement.classList.add('dark')
  })

  // 全局搜索：输入即查，回车跳到第一个结果
  let searchQ = $state('')
  let searchResults = $state<SearchResult[]>([])
  let searchOpen = $state(false)
  let searchTimer: ReturnType<typeof setTimeout> | undefined

  function runSearch() {
    clearTimeout(searchTimer)
    const q = searchQ.trim()
    if (!q) { searchResults = []; searchOpen = false; return }
    searchTimer = setTimeout(async () => {
      try {
        searchResults = await API.get(`/api/v1/search?q=${encodeURIComponent(q)}&limit=6`) as SearchResult[]
        searchOpen = true
      } catch { searchResults = [] }
    }, 200)
  }

  function onSearchKey(e: KeyboardEvent) {
    if (e.key === 'Escape') { searchOpen = false; return }
    if (e.key === 'Enter' && searchResults.length > 0) openResult(searchResults[0])
  }

  function openResult(r: SearchResult) {
    searchOpen = false
    navigate(r.type === 'person' ? r.path : r.path)
  }

  function isActive(path: string) {
    const p = $route.path
    if (path === '/') return p === '/' || p === ''
    return p === path || p.startsWith(path + '/')
  }

  function current() {
    const p = $route.path
    if (p === '/' || p === '') return 'Today'
    if (p === '/people/new') return 'PeopleNew'
    if (p.startsWith('/people/')) {
      const id = p.split('/').pop()!
      return { name: 'PersonDetail', id }
    }
    const map: Record<string, string> = {
      '/people': 'People', '/events': 'Events', '/memos': 'Memos',
      '/money': 'Money', '/anniversaries': 'Anniversaries', '/reminders': 'Reminders',
      '/timeline': 'Timeline', '/graph': 'Graph', '/analytics': 'Analytics', '/settings': 'Settings'
    }
    return map[p] || 'Today'
  }
</script>

<div class="min-h-screen flex">
  <!-- sidebar -->
  <aside class="hidden md:flex w-60 shrink-0 flex-col border-r" style="background: var(--q-surface); border-color: var(--q-border);">
    <div class="px-5 py-5 flex items-center gap-2 border-b" style="border-color: var(--q-border);">
      <div class="w-8 h-8 rounded-xl flex items-center justify-center text-white font-semibold" style="background: var(--q-theme);">牵</div>
      <div>
        <div class="font-semibold leading-none">牵丝</div>
        <div class="text-xs" style="color: var(--q-muted);">人际关系记录</div>
      </div>
    </div>
    <div class="px-3 pt-3">
      <div class="relative">
        <Search size={14} class="absolute left-3 top-1/2 -translate-y-1/2" style="color: var(--q-muted);" />
        <input bind:value={searchQ} oninput={runSearch} onkeydown={onSearchKey} placeholder="搜索人物、往来、对话…"
               class="w-full pl-9 pr-3 py-2 rounded-lg text-sm outline-none"
               style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" />
      </div>
      {#if searchOpen && searchResults.length > 0}
        <ul class="mt-1 rounded-lg overflow-hidden text-sm" style="background: var(--q-surface); border: 1px solid var(--q-border);">
          {#each searchResults as r}
            <li>
              <button class="w-full text-left px-3 py-2 flex items-center gap-2 hover:bg-black/5 dark:hover:bg-white/5"
                      onclick={() => openResult(r)}>
                <span class="text-[10px] px-1.5 py-0.5 rounded shrink-0" style="background: var(--q-bg); color: var(--q-muted);">{SEARCH_LABEL[r.type] || r.type}</span>
                <span class="truncate">{r.title}</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
    <nav class="flex-1 py-3">
      {#each navItems as it}
        {@const Icon = it.icon}
        <a href={it.path}
           class="flex items-center gap-3 px-5 py-2 text-sm transition-colors"
           style="color: var(--q-text); {isActive(it.path) ? 'background: color-mix(in srgb, var(--q-theme) 12%, transparent);' : ''}"
           onclick={() => location.pathname !== it.path && (history.pushState({}, '', it.path), window.dispatchEvent(new PopStateEvent('popstate')))}
           >
          <Icon size={18} />
          <span>{it.label}</span>
        </a>
      {/each}
    </nav>
    <div class="px-5 py-3 text-xs" style="color: var(--q-muted);">v0.1 · local-only</div>
  </aside>

  <!-- mobile top bar -->
  <div class="md:hidden fixed top-0 inset-x-0 z-30 border-b flex items-center justify-between px-4 py-2" style="background: var(--q-surface); border-color: var(--q-border);">
    <div class="font-semibold">牵丝</div>
    <button onclick={() => mobileOpen = !mobileOpen} class="p-1 rounded hover:bg-black/5 dark:hover:bg-white/5">
      {#if mobileOpen}<X size={22} />{:else}<Menu size={22} />{/if}
    </button>
  </div>

  {#if mobileOpen}
    <div class="md:hidden fixed inset-0 top-11 z-20 overflow-y-auto" style="background: var(--q-surface);">
      {#each navItems as it}
        {@const Icon = it.icon}
        <a href={it.path} class="flex items-center gap-3 px-5 py-3 border-b" style="border-color: var(--q-border); color: var(--q-text); {isActive(it.path) ? 'background: color-mix(in srgb, var(--q-theme) 12%, transparent);' : ''}"
           onclick={() => mobileOpen = false}>
          <Icon size={18} />
          <span>{it.label}</span>
        </a>
      {/each}
    </div>
  {/if}

  <!-- main -->
  <main class="flex-1 min-w-0 pt-12 md:pt-0">
    <div class="max-w-6xl mx-auto px-4 md:px-8 py-6 md:py-8">
      {#if typeof current() === 'string'}
        {#if current() === 'Today'}<Today />
        {:else if current() === 'People'}<People />
        {:else if current() === 'PeopleNew'}<People mode="new" />
        {:else if current() === 'Events'}<Events />
        {:else if current() === 'Memos'}<Memos />
        {:else if current() === 'Money'}<Money />
        {:else if current() === 'Anniversaries'}<Anniversaries />
        {:else if current() === 'Reminders'}<Reminders />
        {:else if current() === 'Timeline'}<Events onlyTimeline={true} />
        {:else if current() === 'Graph'}<Graph />
        {:else if current() === 'Analytics'}<Analytics />
        {:else if current() === 'Settings'}<SettingsPage />
        {:else}<Today />
        {/if}
      {:else}
        <PersonDetail id={(current() as any).id} />
      {/if}
    </div>
  </main>
</div>

<style>
  a[href]:hover { background: color-mix(in srgb, var(--q-theme) 8%, transparent); }
</style>
