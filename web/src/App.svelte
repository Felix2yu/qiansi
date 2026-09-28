<script lang="ts">
  import { route, match } from './lib/router'
  import { Home, Users, CalendarDays, MessageCircle, Wallet, Bell, LineChart, Share2, Network, Settings, Menu, X } from '@lucide/svelte'
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
    { label: '纪念日', path: '/anniversaries', icon: Share2 },
    { label: '待办', path: '/reminders', icon: Bell },
    { label: '时间线', path: '/timeline', icon: LineChart },
    { label: '关系图', path: '/graph', icon: Network },
    { label: '统计', path: '/analytics', icon: LineChart },
    { label: '设置', path: '/settings', icon: Settings },
  ]

  let mobileOpen = $state(false)

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
    <nav class="flex-1 py-3">
      {#each navItems as it}
        <a href={it.path} 
           class="flex items-center gap-3 px-5 py-2 text-sm transition-colors"
           style="color: var(--q-text);"
           onclick={() => location.pathname !== it.path && (history.pushState({}, '', it.path), window.dispatchEvent(new PopStateEvent('popstate')))}
           >
          <svelte:component this={it.icon} size={18} />
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
        <a href={it.path} class="flex items-center gap-3 px-5 py-3 border-b" style="border-color: var(--q-border); color: var(--q-text);"
           onclick={() => mobileOpen = false}>
          <svelte:component this={it.icon} size={18} />
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
  a[href].active, a:hover { background: color-mix(in srgb, var(--q-theme) 8%, transparent); }
</style>
