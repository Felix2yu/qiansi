<script lang="ts">
  import { onMount } from 'svelte';
  import {
    API,
    yuan,
    toFen,
    todayLocal,
    KIND_LABEL,
    DIRECTION_LABEL,
    type Transaction,
    type Repayment
  } from '../lib/api';
  import { Plus, Trash2, Wallet } from '@lucide/svelte';
  import Modal from '../lib/ui/Modal.svelte';
  import PageHeader from '../lib/ui/PageHeader.svelte';
  import Empty from '../lib/ui/Empty.svelte';
  import { personLabel, loadSelf } from '../lib/self.svelte';
  import { dict, ensure } from '../lib/dict.svelte';
  import PersonPicker from '../lib/PersonPicker.svelte';
  import { toast } from '../lib/toast.svelte';
  import { ask } from '../lib/ask.svelte';
  import { RECOVER_NOTE, trashOne } from '../lib/trash';

  let list = $state<Transaction[]>([]);
  const events = $derived(dict.events);
  let showForm = $state(false);
  let editId = $state('');
  let form = $state(emptyForm());
  // 编辑时把账目自带的名字交给选人控件显示，省一次回查
  let pickedName = $state('');
  // 还款面板
  let repayOf = $state<Transaction | null>(null);
  let repayList = $state<Repayment[]>([]);
  let repayAmount = $state('');
  let repayDate = $state(todayLocal());
  let repayNote = $state('');

  function emptyForm() {
    return {
      person_id: '',
      kind: 'loan',
      direction: 'out',
      amount_yuan: '',
      title: '',
      occurred_at: todayLocal(),
      due_date: '',
      event_id: '',
      settled: false
    };
  }

  const PAGE = 50;
  let page = $state(0);
  let hasMore = $state(false);

  // 账目以前只能整张表往下翻：想「只看还没收回来的」得自己一行行数。
  let filterPerson = $state('');
  let filterKind = $state('');
  let filterSettled = $state('');
  let filterFrom = $state('');
  let filterTo = $state('');
  const anyFilter = $derived(
    !!(filterPerson || filterKind || filterSettled || filterFrom || filterTo)
  );

  function clearFilters() {
    filterPerson = '';
    filterKind = '';
    filterSettled = '';
    filterFrom = '';
    filterTo = '';
    void load();
  }

  // 同往来页：连着改两个筛选项时只认最新一趟请求的结果
  let reqId = 0;

  async function load(reset = true) {
    if (reset) page = 0;
    const mine = ++reqId;
    const qs = new URLSearchParams({ limit: String(PAGE), offset: String(page * PAGE) });
    if (filterPerson) qs.set('person_id', filterPerson);
    if (filterKind) qs.set('kind', filterKind);
    if (filterSettled) qs.set('settled', filterSettled);
    if (filterFrom) qs.set('from', filterFrom);
    if (filterTo) qs.set('to', filterTo);
    const batch = await API.get<Transaction[]>(`/api/v1/transactions?${qs}`).catch((err: any) => {
      toast.fail('加载失败', err);
      return [] as Transaction[];
    });
    if (mine !== reqId) return;
    hasMore = batch.length === PAGE;
    list = reset ? batch : [...list, ...batch];
  }
  async function loadMore() {
    page += 1;
    await load(false);
  }
  onMount(() => {
    loadSelf();
    ensure('events');
    load(true);
  });

  function openCreate() {
    editId = '';
    form = emptyForm();
    pickedName = '';
    showForm = true;
  }

  async function openEdit(t: Transaction) {
    let full: Transaction;
    try {
      full = (await API.get(`/api/v1/transactions/${t.id}`)) as Transaction;
    } catch (err) {
      toast.fail('读取账目失败', err);
      return;
    }
    editId = full.id;
    pickedName = full.person_name || '';
    form = {
      person_id: full.person_id,
      kind: full.kind,
      direction: full.direction,
      amount_yuan: yuan(full.amount_fen),
      title: full.title || '',
      occurred_at: (full.occurred_at || '').slice(0, 10),
      due_date: full.due_date ? full.due_date.slice(0, 10) : '',
      event_id: full.event_id || '',
      settled: !!full.settled
    };
    showForm = true;
  }

  function closeForm() {
    showForm = false;
    editId = '';
  }

  async function submit() {
    const fen = toFen(form.amount_yuan);
    if (!form.person_id || fen <= 0) {
      toast.error('请选择联系人并填写大于 0 的金额（元）');
      return;
    }
    // 只有借还才有「未结清」：礼物/花销提交即了结，避免被算进欠账口径
    const body: any = {
      person_id: form.person_id,
      kind: form.kind,
      direction: form.direction,
      amount_fen: fen,
      title: form.title,
      occurred_at: form.occurred_at || todayLocal(),
      due_date: form.due_date || '',
      event_id: form.event_id || '',
      settled: form.kind === 'loan' ? !!form.settled : true
    };
    const isEdit = !!editId;
    try {
      if (isEdit) await API.put(`/api/v1/transactions/${editId}`, body);
      else await API.post('/api/v1/transactions', body);
    } catch (err) {
      toast.fail('保存失败', err);
      return;
    }
    closeForm();
    toast.ok(isEdit ? '已更新' : '已记一笔');
    await load();
  }

  async function remove(id: string) {
    if (
      !(await ask({
        title: '删除这笔记录？',
        detail: '这笔账目连同它的还款登记一起进回收站，' + RECOVER_NOTE,
        danger: true,
        confirmLabel: '删除'
      }))
    )
      return;
    await trashOne('transaction', id, load, '已删除');
  }

  // 名字随账目一起回来，不再为了这几个字拉全量名单
  function who(t: { person_id: string; person_name?: string }) {
    return personLabel({ id: t.person_id, name: t.person_name || '（已删除）' });
  }
  function remaining(t: Transaction) {
    return Math.max(0, t.amount_fen - (t.repaid_fen || 0));
  }

  async function refreshRepay() {
    if (!repayOf) return;
    repayList = (await API.get(`/api/v1/transactions/${repayOf.id}/repayments`)) as Repayment[];
    repayOf = (await API.get(`/api/v1/transactions/${repayOf.id}`)) as Transaction;
  }

  async function openRepay(t: Transaction) {
    repayOf = t;
    repayAmount = '';
    repayDate = todayLocal();
    repayNote = '';
    try {
      await refreshRepay();
    } catch (err) {
      toast.fail('读取还款记录失败', err);
    }
  }

  async function submitRepay() {
    if (!repayOf) return;
    const fen = toFen(repayAmount);
    if (fen <= 0) {
      toast.error('请填写还款金额（元）');
      return;
    }
    const txId = repayOf.id;
    try {
      await API.post(`/api/v1/transactions/${txId}/repayments`, {
        amount_fen: fen,
        occurred_at: repayDate || todayLocal(),
        note: repayNote
      });
      // 还清则自动标记为结清
      const fresh = (await API.get(`/api/v1/transactions/${txId}`)) as Transaction;
      if (remaining(fresh) === 0 && !fresh.settled) {
        await API.put(`/api/v1/transactions/${fresh.id}`, { ...fresh, settled: true });
      }
      repayAmount = '';
      repayNote = '';
      await load();
      await refreshRepay();
      toast.ok('已登记还款');
    } catch (err) {
      toast.fail('还款登记失败', err);
      await refreshRepay().catch(() => {});
    }
  }

  async function removeRepay(id: string) {
    if (!repayOf) return;
    if (
      !(await ask({
        title: '删除这条还款记录？',
        detail: '删除后这笔借还的已还金额会回退。',
        danger: true,
        confirmLabel: '删除'
      }))
    )
      return;
    try {
      await API.delete(`/api/v1/transactions/${repayOf.id}/repayments/${id}`);
      await load();
      await refreshRepay();
      toast.ok('已删除');
    } catch (err) {
      toast.fail('删除失败', err);
    }
  }
  // 只翻转结清标记（用于无需逐笔登记还款的场景）
  async function toggleSettled(id: string) {
    let t: Transaction;
    try {
      t = (await API.get(`/api/v1/transactions/${id}`)) as Transaction;
      const settled = !t.settled;
      await API.put(`/api/v1/transactions/${id}`, { ...t, settled });
      // 翻回去就是撤销，不必再点一次
      toast.undoable(settled ? '已结清' : '已取消结清', async () => {
        await API.put(`/api/v1/transactions/${id}`, { ...t, settled: !settled }).catch(() => {});
        await load();
      });
      await load();
    } catch (err) {
      toast.fail('操作失败', err);
    }
  }
</script>

<div class="q-page">
  <PageHeader title="金钱往来" subtitle="借款、还款、礼物、花销">
    {#snippet actions()}
      <button class="q-btn q-btn--primary q-btn--sm" onclick={openCreate}>
        <Plus size={14} /> 新建
      </button>
    {/snippet}
  </PageHeader>

  <div class="q-stack">
    <!-- 筛选工具条 -->
    <div class="q-card q-card--tight flex flex-wrap items-center gap-2">
      <div class="w-48">
        <PersonPicker
          bind:value={filterPerson}
          placeholder="全部联系人"
          compact={true}
          onchange={() => load()}
        />
      </div>
      <select
        bind:value={filterKind}
        onchange={() => load()}
        aria-label="按类别筛选"
        class="q-select !py-2"
      >
        <option value="">全部类别</option>
        <option value="loan">借还</option>
        <option value="gift">礼物</option>
        <option value="expense">花销</option>
        <option value="other">其它</option>
      </select>
      <select
        bind:value={filterSettled}
        onchange={() => load()}
        aria-label="按结清状态筛选"
        class="q-select !py-2"
      >
        <option value="">结清与否</option>
        <option value="0">未结清</option>
        <option value="1">已结清</option>
      </select>
      <div class="flex items-center gap-1.5">
        <input
          type="date"
          bind:value={filterFrom}
          onchange={() => load()}
          aria-label="起始日期"
          class="q-input !py-2"
        />
        <span class="text-xs" style="color: var(--q-muted);">至</span>
        <input
          type="date"
          bind:value={filterTo}
          onchange={() => load()}
          aria-label="截止日期"
          class="q-input !py-2"
        />
      </div>
      {#if anyFilter}
        <button class="q-btn q-btn--ghost q-btn--sm" onclick={clearFilters}>清除筛选</button>
      {/if}
    </div>

    <div class="q-stack--tight flex flex-col">
      {#each list as t (t.id)}
        {@const out = t.direction === 'out'}
        <div class="q-row">
          <div
            class="q-avatar h-10 w-10 text-sm"
            style={`background: color-mix(in srgb, ${out ? 'var(--q-c-money)' : 'var(--q-ok)'} 16%, var(--q-surface)); color: ${out ? 'var(--q-c-money)' : 'var(--q-ok)'};`}
          >
            {out ? '出' : '入'}
          </div>
          <div class="min-w-0 flex-1">
            <div class="truncate text-sm font-medium">
              {who(t)}{t.title && ` · ${t.title}`}
            </div>
            <div class="q-num mt-0.5 truncate text-xs" style="color: var(--q-muted);">
              ¥{yuan(t.amount_fen)} · {KIND_LABEL[t.kind] || t.kind} ·
              {DIRECTION_LABEL[t.direction] || t.direction} ·
              {new Date(t.occurred_at).toLocaleDateString()}
              {#if t.kind === 'loan'}
                {t.settled ? ' · 已结清' : ''}
                {#if !t.settled && (t.repaid_fen || 0) > 0}
                  · 已还 ¥{yuan(t.repaid_fen || 0)}，待还 ¥{yuan(remaining(t))}
                {/if}
              {/if}
              {t.event_title && ` · 关联事件：${t.event_title}`}
            </div>
          </div>
          <div
            class="q-num shrink-0 text-sm font-semibold"
            style={`color: ${out ? 'var(--q-c-money)' : 'var(--q-ok)'};`}
          >
            {out ? '-' : '+'}¥{yuan(t.amount_fen)}
          </div>
          <div class="flex shrink-0 items-center gap-1">
            {#if t.kind === 'loan'}
              <button class="q-btn q-btn--ghost q-btn--sm" onclick={() => openRepay(t)}>还款</button
              >
              <button class="q-btn q-btn--ghost q-btn--sm" onclick={() => toggleSettled(t.id)}>
                {t.settled ? '取消结清' : '结清'}
              </button>
            {/if}
            <button class="q-btn q-btn--ghost q-btn--sm" onclick={() => openEdit(t)}>编辑</button>
            <button class="q-iconbtn !h-8 !w-8" onclick={() => remove(t.id)} title="删除">
              <Trash2 size={14} />
            </button>
          </div>
        </div>
      {:else}
        <Empty
          icon={Wallet}
          text={anyFilter ? '没有符合条件的记录' : '暂无记录，记一笔借还或礼物'}
        />
      {/each}
    </div>

    {#if hasMore}
      <div class="text-center">
        <button class="q-btn q-btn--ghost" onclick={loadMore}>
          加载更多（已显示 {list.length} 条）
        </button>
      </div>
    {/if}
  </div>
</div>

{#if showForm}
  <Modal wide onclose={closeForm} title={editId ? '编辑账目' : '新建账目'}>
    <div class="q-stack--tight flex flex-col">
      <PersonPicker
        bind:value={form.person_id}
        selectedName={pickedName}
        placeholder="选择联系人"
        clearable={false}
      />
      <div class="grid grid-cols-2 gap-2">
        <select bind:value={form.kind} class="q-select">
          <option value="loan">借还</option>
          <option value="gift">礼物</option>
          <option value="expense">花销</option>
          <option value="other">其它</option>
        </select>
        <select bind:value={form.direction} class="q-select">
          <option value="out">我支出</option>
          <option value="in">我收入</option>
        </select>
      </div>
      <input
        type="number"
        step="0.01"
        min="0"
        bind:value={form.amount_yuan}
        class="q-input"
        placeholder="金额（元）"
      />
      <input bind:value={form.title} class="q-input" placeholder="标题（如：生日礼物）" />
      <div class="grid grid-cols-2 gap-2">
        <input type="date" bind:value={form.occurred_at} class="q-input" />
        <input type="date" bind:value={form.due_date} class="q-input" placeholder="到期（可选）" />
      </div>
      <select bind:value={form.event_id} class="q-select">
        <option value="">关联事件（可选）</option>
        {#each events as e (e.id)}
          <option value={e.id}>{new Date(e.event_date).toLocaleDateString()} · {e.title}</option>
        {/each}
      </select>
      {#if form.kind === 'loan'}
        <label
          class="flex cursor-pointer items-center gap-2 text-sm select-none"
          style="color: var(--q-muted);"
        >
          <input type="checkbox" bind:checked={form.settled} /> 已结清
        </label>
      {:else}
        <p class="q-hint !mt-0">礼物/花销提交即视为了结，不计入未还金额</p>
      {/if}
    </div>
    {#snippet foot()}
      <button class="q-btn q-btn--ghost" onclick={closeForm}>取消</button>
      <button class="q-btn q-btn--primary" onclick={submit}>保存</button>
    {/snippet}
  </Modal>
{/if}

{#if repayOf}
  <Modal wide onclose={() => (repayOf = null)} title={`还款流水 · ${who(repayOf)}`}>
    <div class="q-stack">
      <div class="q-well q-num text-sm" style="color: var(--q-muted);">
        本金 ¥{yuan(repayOf.amount_fen)} · 已还 ¥{yuan(repayOf.repaid_fen || 0)} · 待还
        <span class="font-semibold" style="color: var(--q-text);">¥{yuan(remaining(repayOf))}</span>
      </div>
      <ul class="q-stack--tight flex max-h-48 flex-col overflow-auto">
        {#each repayList as r (r.id)}
          <li class="q-row !py-2 text-sm">
            <span class="q-num">¥{yuan(r.amount_fen)}</span>
            <span class="q-num" style="color: var(--q-muted);">
              {r.occurred_at}{r.note ? ` · ${r.note}` : ''}
            </span>
            <button
              class="q-iconbtn ml-auto !h-7 !w-7"
              onclick={() => removeRepay(r.id)}
              title="删除"
            >
              <Trash2 size={13} />
            </button>
          </li>
        {:else}
          <li class="py-1 text-sm" style="color: var(--q-muted);">还没有还款记录</li>
        {/each}
      </ul>
      <div class="grid grid-cols-3 gap-2">
        <input
          type="number"
          step="0.01"
          min="0"
          bind:value={repayAmount}
          placeholder="本次还款（元）"
          class="q-input"
        />
        <input type="date" bind:value={repayDate} class="q-input" />
        <input bind:value={repayNote} placeholder="备注（可选）" class="q-input" />
      </div>
    </div>
    {#snippet foot()}
      <button class="q-btn q-btn--ghost" onclick={() => (repayOf = null)}>关闭</button>
      <button class="q-btn q-btn--primary" onclick={submitRepay}>记一笔</button>
    {/snippet}
  </Modal>
{/if}
