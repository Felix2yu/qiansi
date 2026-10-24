<script lang="ts">
  import { onMount } from 'svelte';
  import { API, contactAgo, gradeLabel, daysSince, type DriftPerson } from '../lib/api';
  import { navigate } from '../lib/router';
  import PageHeader from '../lib/ui/PageHeader.svelte';
  import Empty from '../lib/ui/Empty.svelte';
  import { HeartPulse, PenLine } from '@lucide/svelte';
  import { toast } from '../lib/toast.svelte';

  let list = $state<DriftPerson[]>([]);
  let onlyOverdue = $state(true);
  let loading = $state(true);
  // 连点两个筛选会并发发出两趟请求，谁后回来谁说了算——只认最新那次
  let reqId = 0;

  async function load() {
    const mine = ++reqId;
    loading = true;
    const qs = onlyOverdue ? '?only=overdue&limit=200' : '?limit=200';
    const got =
      (await API.get<DriftPerson[]>(`/api/v1/contacts/drift${qs}`).catch((err: any) => {
        toast.fail('加载失败', err);
        return [] as DriftPerson[];
      })) || [];
    if (mine !== reqId) return;
    list = got;
    loading = false;
  }

  onMount(load);

  // 后端给的是「距锚点几天」与「逾期几天」，还剩几天从到期日现算
  function untilDue(d: DriftPerson): number | null {
    const n = daysSince(d.due_at);
    return n === null ? null : -n;
  }

  async function checkin(d: DriftPerson) {
    try {
      await API.post(`/api/v1/contacts/${d.person_id}/checkin`, {});
    } catch (err) {
      // 名单上这个人可能已经被删进回收站了，重拉一次让屏幕跟上服务端
      toast.fail('打卡失败', err);
      await load();
      return;
    }
    toast.ok(`已记下今天联系过 ${d.name}`);
    await load();
  }

  function rowMeta(d: DriftPerson): string {
    const from =
      d.anchor_from === 'contact'
        ? `上次联系 ${contactAgo(d.anchor)}`
        : `建档于 ${contactAgo(d.anchor)}`;
    return `${gradeLabel(d.grade)} · ${from} · 每 ${d.days} 天`;
  }
</script>

<div class="q-page">
  <PageHeader title="渐远名单" subtitle="按亲密度设定的节奏，算出谁该联系了">
    {#snippet actions()}
      <button class="q-btn q-btn--ghost q-btn--sm" onclick={() => navigate('/settings')}>
        改联系节奏
      </button>
    {/snippet}
  </PageHeader>

  <div class="q-stack">
    <div class="q-seg">
      {#each [{ v: true, n: '已逾期' }, { v: false, n: '全部' }] as tab (tab.v)}
        <button
          onclick={() => {
            onlyOverdue = tab.v;
            load();
          }}
          aria-pressed={onlyOverdue === tab.v}
          class="q-seg__btn"
        >
          {tab.n}{#if tab.v && !loading}<span class="q-num ml-1 opacity-80">{list.length}</span
            >{/if}
        </button>
      {/each}
    </div>

    {#if loading}
      <div class="py-12 text-center text-sm" style="color: var(--q-muted);">加载中…</div>
    {:else if list.length === 0}
      <Empty
        icon={HeartPulse}
        text={onlyOverdue
          ? '没有到期的人。要连同还没到期的名单一起看，切到「全部」。'
          : '还没有联系人。'}
      />
    {:else}
      <div class="q-stack--tight flex flex-col">
        {#each list as d (d.person_id)}
          <div class="q-row flex-wrap">
            <div class="min-w-0 flex-1">
              <button
                class="link-quiet truncate text-sm font-medium"
                onclick={() => navigate(`/people/${d.person_id}`)}>{d.name}</button
              >
              <div class="q-num mt-0.5 truncate text-xs" style="color: var(--q-muted);">
                {rowMeta(d)}
              </div>
            </div>
            {#if d.overdue}
              <span class="q-badge q-badge--danger shrink-0">
                <HeartPulse size={12} /> 已逾期 {d.overdue_days} 天
              </span>
            {:else}
              {@const left = untilDue(d)}
              <span class="q-badge shrink-0">
                {left === null ? d.due_at : left === 0 ? '今天该联系' : `还有 ${left} 天`}
              </span>
            {/if}
            <button class="q-btn q-btn--ok q-btn--sm shrink-0" onclick={() => checkin(d)}>
              联系过了
            </button>
            <button
              class="q-btn q-btn--ghost q-btn--sm shrink-0"
              onclick={() => navigate(`/events?new=1&with=${d.person_id}`)}
            >
              <PenLine size={12} /> 记一笔
            </button>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>
