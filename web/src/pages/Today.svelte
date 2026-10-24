<script lang="ts">
  import { onMount } from 'svelte';
  import {
    API,
    todayLocal,
    REF_TYPE_LABEL,
    type Dashboard,
    type Reminder,
    type Suggestion
  } from '../lib/api';
  import { navigate } from '../lib/router';
  import { toast } from '../lib/toast.svelte';
  import { completeReminder } from '../lib/reminderActions';
  import { self, loadSelf } from '../lib/self.svelte';
  import PageHeader from '../lib/ui/PageHeader.svelte';
  import Empty from '../lib/ui/Empty.svelte';
  import {
    CalendarDays,
    Users,
    Wallet,
    Bell,
    Sparkles,
    CheckCircle2,
    ArrowRight,
    Check,
    X,
    CalendarCheck
  } from '@lucide/svelte';

  const today = todayLocal();
  let dashboard = $state<Dashboard | null>(null);
  let upcoming = $state<Reminder[]>([]);
  let suggestions = $state<Suggestion[]>([]);
  let loading = $state(true);
  // 首启引导：三步全靠已有数据判定，不做「已读」标记，走完自然就不出现了。
  // 唯一例外是用户明确说不用教——那就在本机收起，别让它赖在首屏上。
  let onboardHidden = $state(localStorage.getItem('q_onboard_off') === '1');
  const onboardSteps = $derived([
    {
      label: '先把第一个人记进来',
      hint: '通讯录是这一切的起点',
      done: (dashboard?.total_people || 0) > 0,
      to: '/people/new',
      cta: '去新建'
    },
    {
      label: '说出哪一条记录是你自己',
      hint: '关系图拼色、认识路径都以此为原点',
      done: !!self.id,
      to: '/settings',
      cta: '去设置'
    },
    {
      label: '记一次往来',
      hint: '吃过饭、随过礼都算一场',
      done: (dashboard?.total_events || 0) > 0,
      to: '/events?new=1',
      cta: '去记一笔'
    }
  ]);
  const showOnboard = $derived(!onboardHidden && onboardSteps.some((s) => !s.done));
  function hideOnboard() {
    onboardHidden = true;
    localStorage.setItem('q_onboard_off', '1');
  }
  // 首页只给「要紧的」：逾期与今天，其余留在下方或待办页
  const imminent = $derived((upcoming || []).filter((r) => (r.due_at || '').slice(0, 10) <= today));
  const later = $derived((upcoming || []).filter((r) => (r.due_at || '').slice(0, 10) > today));
  async function load() {
    loading = true;
    try {
      const [d, r, s] = await Promise.all([
        API.get('/api/v1/dashboard/stats'),
        API.get('/api/v1/reminders/upcoming?days=30'),
        API.get('/api/v1/dashboard/suggestions')
      ]);
      dashboard = d as Dashboard;
      upcoming = r as Reminder[];
      suggestions = s as Suggestion[];
    } catch (err) {
      // 拉不到要留在屏幕上：首页一片空白和真的没有数据看起来一样
      toast.fail('加载失败', err);
    } finally {
      loading = false;
    }
  }
  onMount(() => {
    loadSelf();
    load();
  });
  function done(r: Reminder) {
    void completeReminder(r, load);
  }
</script>

<div class="q-page">
  <PageHeader title="今日" subtitle="牵丝 · 人际关系一目了然" />

  {#if loading}
    <div class="py-16 text-center" style="color: var(--q-muted);">加载中…</div>
  {:else if dashboard}
    <div class="q-stack">
      {#if showOnboard}
        <section class="q-card">
          <div class="mb-3 flex items-start justify-between gap-2">
            <h2 class="q-section-title !mb-0">
              <Sparkles size={15} /> 三步开始用牵丝
            </h2>
            <button class="q-iconbtn !h-7 !w-7" title="不用了" onclick={hideOnboard}>
              <X size={14} />
            </button>
          </div>
          <ol class="q-stack--tight flex flex-col">
            {#each onboardSteps as s, i (i)}
              <li class="q-well flex items-center gap-3 !py-2.5">
                {#if s.done}
                  <span
                    class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full"
                    style="background: var(--q-ok); color: #f4faf5;"
                  >
                    <Check size={12} />
                  </span>
                {:else}
                  <span
                    class="q-num flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-xs"
                    style="border: 1px solid var(--q-border-strong); color: var(--q-muted);"
                  >
                    {i + 1}
                  </span>
                {/if}
                <div class="min-w-0 flex-1">
                  <div
                    class="text-sm"
                    class:line-through={s.done}
                    style={s.done ? 'color: var(--q-muted);' : ''}
                  >
                    {s.label}
                  </div>
                  <div class="text-xs" style="color: var(--q-muted);">{s.hint}</div>
                </div>
                {#if !s.done}
                  <button
                    class="q-btn q-btn--primary q-btn--sm shrink-0"
                    onclick={() => navigate(s.to)}
                  >
                    {s.cta}
                    <ArrowRight size={11} />
                  </button>
                {/if}
              </li>
            {/each}
          </ol>
        </section>
      {/if}

      <section class="grid grid-cols-2 gap-3 md:grid-cols-4">
        <div class="q-stat">
          <div class="q-stat__label flex items-center gap-1.5">
            <Users size={13} /> 联系人
          </div>
          <div class="q-stat__value q-num">{dashboard.total_people}</div>
        </div>
        <div class="q-stat">
          <div class="q-stat__label flex items-center gap-1.5">
            <CalendarDays size={13} /> 往来事件
          </div>
          <div class="q-stat__value q-num">{dashboard.total_events}</div>
        </div>
        <div class="q-stat">
          <div class="q-stat__label flex items-center gap-1.5">
            <Bell size={13} /> 待办 (7 天)
          </div>
          <div class="q-stat__value q-num">{dashboard.upcoming_days7}</div>
        </div>
        <div class="q-stat">
          <div class="q-stat__label flex items-center gap-1.5">
            <CheckCircle2 size={13} /> 未兑现承诺
          </div>
          <div class="q-stat__value q-num">{dashboard.pending_promises}</div>
        </div>
      </section>

      <section class="grid grid-cols-2 gap-3">
        <div class="q-stat">
          <div class="q-stat__label flex items-center gap-1.5">
            <Wallet size={13} /> 我借出（未还）
          </div>
          <div class="q-stat__value q-num" style="color: var(--q-ok);">
            ¥ {(dashboard.lend_fen / 100).toFixed(2)}
          </div>
        </div>
        <div class="q-stat">
          <div class="q-stat__label flex items-center gap-1.5">
            <Wallet size={13} /> 我待还
          </div>
          <div class="q-stat__value q-num" style="color: var(--q-warn);">
            ¥ {(dashboard.borrow_fen / 100).toFixed(2)}
          </div>
        </div>
      </section>

      {#if suggestions.length > 0}
        <section>
          <h2 class="q-section-title"><Sparkles size={15} /> 智能建议</h2>
          <div class="q-stack--tight flex flex-col">
            {#each suggestions.slice(0, 6) as s, i (i)}
              <button
                class="q-card q-card--tight flex w-full items-start gap-3 text-left"
                disabled={!s.person_id}
                onclick={() => s.person_id && navigate(`/people/${s.person_id}`)}
              >
                <span class="q-badge q-badge--theme mt-0.5 shrink-0">{s.type}</span>
                <div class="min-w-0 flex-1 text-sm">
                  <div class="truncate font-medium">{s.person_name || '系统'}</div>
                  <div style="color: var(--q-muted);">{s.message}</div>
                </div>
              </button>
            {/each}
          </div>
        </section>
      {/if}

      <section>
        <div class="mb-3 flex items-center justify-between">
          <h2 class="q-section-title !mb-0"><CalendarCheck size={15} /> 近期待办</h2>
          <button class="q-btn q-btn--subtle q-btn--sm" onclick={() => navigate('/reminders')}>
            查看全部 <ArrowRight size={12} />
          </button>
        </div>
        {#if upcoming.length === 0}
          <Empty icon={Bell} text="近期没有待办，正好去见见想见的人" />
        {:else}
          {#if imminent.length > 0}
            <div class="mt-1 mb-2 flex items-center gap-2">
              <span class="q-badge q-badge--danger">逾期 / 今天 · {imminent.length}</span>
            </div>
            <div class="q-stack--tight mb-4 flex flex-col">
              {#each imminent as r, i (i)}
                <label class="q-row cursor-pointer">
                  <input type="checkbox" class="shrink-0" onchange={() => done(r)} />
                  <div class="min-w-0 flex-1">
                    <div class="truncate text-sm">{r.title}</div>
                    <div class="q-num mt-0.5 truncate text-xs" style="color: var(--q-muted);">
                      {REF_TYPE_LABEL[r.ref_type] || r.ref_type}{r.person_name &&
                        ` · ${r.person_name}`} · {new Date(r.due_at).toLocaleDateString()}
                    </div>
                  </div>
                </label>
              {/each}
            </div>
          {/if}
          {#if later.length > 0}
            <div class="mb-2 flex items-center gap-2">
              <span class="q-badge">之后 · {later.length}</span>
            </div>
            <div class="q-stack--tight flex flex-col">
              {#each later.slice(0, 6) as r, i (i)}
                <label class="q-row cursor-pointer">
                  <input type="checkbox" class="shrink-0" onchange={() => done(r)} />
                  <div class="min-w-0 flex-1">
                    <div class="truncate text-sm">{r.title}</div>
                    <div class="q-num mt-0.5 truncate text-xs" style="color: var(--q-muted);">
                      {REF_TYPE_LABEL[r.ref_type] || r.ref_type}{r.person_name &&
                        ` · ${r.person_name}`} · {new Date(r.due_at).toLocaleDateString()}
                    </div>
                  </div>
                </label>
              {/each}
            </div>
          {/if}
        {/if}
      </section>
    </div>
  {/if}
</div>
