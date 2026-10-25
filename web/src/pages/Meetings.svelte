<script lang="ts">
  import { onMount } from 'svelte';
  import { API, gradeLabel, type MeetingOverviewRow } from '../lib/api';
  import { navigate } from '../lib/router';
  import { loadSelf, self } from '../lib/self.svelte';
  import PageHeader from '../lib/ui/PageHeader.svelte';
  import Empty from '../lib/ui/Empty.svelte';
  import LoadError from '../lib/ui/LoadError.svelte';
  import { Handshake, PenLine } from '@lucide/svelte';
  import { toast, errText } from '../lib/toast.svelte';

  let list = $state<MeetingOverviewRow[]>([]);
  let loading = $state(true);
  // 加载失败与「还没见过谁」必须分开显示：前者是没读到，后者是真的没有
  let loadError = $state('');
  let reqId = 0;

  async function load() {
    const mine = ++reqId;
    loading = true;
    try {
      const got = await API.get<MeetingOverviewRow[]>('/api/v1/meetings/overview');
      if (mine !== reqId) return;
      // 「我和联系人的见面」——本人是另一头的锚点，不该出现在被约的名单里
      list = got.filter((r) => r.person_id !== self.id);
      loadError = '';
    } catch (err) {
      if (mine !== reqId) return;
      toast.fail('加载失败', err);
      loadError = errText(err);
      list = [];
    } finally {
      if (mine === reqId) loading = false;
    }
  }

  onMount(async () => {
    // 先拿本人再拉名单：两个请求赛跑时 self.id 还没就位，「我」会混进被约名单
    await loadSelf();
    await load();
  });

  function rowMeta(r: MeetingOverviewRow): string {
    const grade = gradeLabel(r.grade);
    // 间隔为 0 只说明多次见面发生在同一天，不代表「只见过一次」
    const gap = r.avg_gap_days ?? 0;
    const freq =
      r.meeting_count <= 1 ? '只见过一次' : gap > 0 ? `平均每 ${gap} 天` : '都发生在同一天';
    return `${grade} · 见过 ${r.meeting_count} 次 · ${freq}`;
  }

  // 超过自己的平均间隔就算「该约了」，用警示色点出来
  function overPace(r: MeetingOverviewRow): boolean {
    return !!r.avg_gap_days && r.days_since_last > r.avg_gap_days;
  }
</script>

<div class="q-page">
  <PageHeader
    title="见面总览"
    subtitle="按「算见面」的往来统计：上次见面、次数与平均间隔，最久没见的排最前"
  >
    {#snippet actions()}
      <button class="q-btn q-btn--ghost q-btn--sm" onclick={() => navigate('/settings')}>
        调见面口径
      </button>
    {/snippet}
  </PageHeader>

  <div class="q-stack">
    {#if loading}
      <div class="py-12 text-center text-sm" style="color: var(--q-muted);">加载中…</div>
    {:else if list.length === 0}
      {#if loadError}
        <LoadError text={`加载失败：${loadError}`} onretry={() => load()} />
      {:else}
        <Empty icon={Handshake} text="还没有见过面的记录。记一场往来并选上 TA，就会出现在这里。" />
      {/if}
    {:else}
      <div class="q-stack--tight flex flex-col">
        {#each list as r (r.person_id)}
          <div class="q-row">
            <div class="min-w-0 flex-1">
              <button
                class="link-quiet truncate text-sm font-medium"
                onclick={() => navigate(`/people/${r.person_id}`)}>{r.name}</button
              >
              <div class="q-num mt-0.5 truncate text-xs" style="color: var(--q-muted);">
                {rowMeta(r)}
              </div>
            </div>
            <span
              class="q-badge shrink-0"
              class:q-badge--danger={overPace(r)}
              title={overPace(r)
                ? `已超过你们平均 ${r.avg_gap_days} 天的节奏`
                : `上次见面 ${r.last_meeting}`}
            >
              {#if r.days_since_last === 0}
                今天刚见
              {:else}
                {r.days_since_last} 天没见
              {/if}
            </span>
            <button
              class="q-btn q-btn--ghost q-btn--sm shrink-0"
              onclick={() => navigate(`/events?new=1&with=${r.person_id}`)}
            >
              <PenLine size={12} /> 记一笔
            </button>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>
