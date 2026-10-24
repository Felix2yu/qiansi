<script lang="ts">
  import { untrack } from 'svelte';
  import { API, todayLocal, toFen, yuan, type Event, type EventType, type EventBill } from './api';
  import { selfFirst, personLabel, loadSelf, isSelf } from './self.svelte';
  import { dict, ensure } from './dict.svelte';
  import { searchPeople, resolvePeople, type PersonLite } from './personSearch';
  import PersonPicker from './PersonPicker.svelte';
  import BillPicker from './BillPicker.svelte';
  import { toast } from './toast.svelte';
  import { Trash2, Search, X, ExternalLink, Link2 } from '@lucide/svelte';

  let {
    editId = '',
    preset = {},
    types = null,
    onsave,
    oncancel,
    onremove
  }: {
    /** 非空进入编辑模式，组件自行拉取详情 */
    editId?: string;
    /** 新建时的预填：默认日期、默认参与人 */
    preset?: { event_date?: string; participant_ids?: string[] };
    /** 传入可复用父级已加载的数据，不传则自行请求 */
    types?: EventType[] | null;
    onsave?: () => void;
    oncancel?: () => void;
    /** 提供后编辑态显示删除按钮，由父级确认并删除 */
    onremove?: (id: string, title: string, expenseFen?: number) => void;
  } = $props();

  const typeList = $derived(types ?? dict.eventTypes);

  let loading = $state(false);
  let saving = $state(false);
  // 编辑时原有关联开销，用于删除时提示是否解挂账目
  let originExpenseFen = $state(0);
  // 参与人较多时不能平铺全量，这里按关键字搜
  let peopleQuery = $state('');
  let candidates = $state<PersonLite[]>([]);
  let searching = $state(false);
  let searchFailed = $state(false);
  // 已选参与人要能显示名字，详情与搜索结果都往这里存一份
  let known = $state<Record<string, PersonLite>>({});
  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  // 必须声明在下方 untrack() 之前：初始化时就会立即 runSearch()，
  // 而 `let` 有 TDZ，放到下面会抛 "Cannot access before initialization"，
  // 表现是参与人候选一直空着（列表显示「没有匹配的联系人」）。
  let searchSeq = 0;
  // 关联的货殖账单。账单是开销的权威来源，一旦有账单，手填金额就不再提交。
  let bills = $state<EventBill[]>([]);
  let showBillPicker = $state(false);
  let billBusy = $state(false);

  // 只数支出账单：与后端 eventColumns 的口径一致（入账/转账不是这次往来的花费）
  const billsSum = $derived(
    bills.reduce((n, b) => (b.type === 'expense' ? n + (b.amount_fen || 0) : n), 0)
  );
  const hasBills = $derived(bills.length > 0);

  type Form = {
    title: string;
    type_id: number;
    event_date: string;
    locations: string[];
    has_gift: boolean;
    gift: string;
    summary: string;
    participant_ids: string[];
    expense_yuan: string;
    expense_person_id: string;
    gift_yuan: string;
    gift_direction: string;
    gift_person_id: string;
  };

  function blank(): Form {
    return {
      title: '',
      type_id: 0,
      event_date: preset?.event_date || todayLocal(),
      locations: [''],
      has_gift: false,
      gift: '',
      summary: '',
      participant_ids: [...(preset?.participant_ids || [])],
      expense_yuan: '',
      expense_person_id: '',
      gift_yuan: '',
      gift_direction: 'out',
      gift_person_id: ''
    };
  }

  let form = $state<Form>(blank());
  // 礼金金额一变就决定要不要问归属人，省得空着时也摊一排下拉框
  const giftFen = $derived(toFen(form.gift_yuan));

  // 切换新建 / 编辑目标时刷新表单；seq 用于丢弃过期的详情响应
  let loadSeq = 0;
  $effect(() => {
    if (editId) void loadDetail(editId);
    else {
      form = blank();
      originExpenseFen = 0;
      peopleQuery = '';
      bills = [];
    }
  });

  // 父级没传数据时读共享字典，避免每个调用方都写一遍（只看初始值，故用 untrack）
  untrack(() => {
    void loadSelf();
    if (!types) void ensure('eventTypes');
    void runSearch();
    if (preset.participant_ids?.length) void remember(preset.participant_ids);
  });

  async function loadDetail(id: string) {
    const seq = ++loadSeq;
    loading = true;
    try {
      const d = (await API.get(`/api/v1/events/${id}`)) as Event;
      if (seq !== loadSeq) return;
      // 开销只认事件自己认领的那笔（expense_transaction_id 指针）。
      // 用户从金钱页手工挂上来的 expense 也计入 expense_fen 合计，但表单不能
      // 把它们预填成「事件的开销」——再保存一次就等于把别人的账复制成自己的。
      const own = d.expense_transaction_id
        ? (d.expenses || []).find((t) => t.id === d.expense_transaction_id)
        : undefined;
      originExpenseFen = d.expense_fen || own?.amount_fen || 0;
      bills = d.bills || [];
      form = {
        title: d.title,
        type_id: d.type_id ?? 0,
        event_date: (d.event_date || '').slice(0, 10),
        locations: d.locations && d.locations.length > 0 ? [...d.locations] : [d.location || ''],
        has_gift: !!d.has_gift,
        gift: d.gift || '',
        summary: d.summary || '',
        participant_ids: (d.participants || []).map((p) => p.id),
        expense_yuan: own ? (own.amount_fen / 100).toFixed(2) : '',
        expense_person_id: own?.person_id || '',
        // 礼金三项都从事件自带那笔账上读回，编辑时不改动就不该再复制一条
        gift_yuan: d.gift_amount_fen ? (d.gift_amount_fen / 100).toFixed(2) : '',
        gift_direction: d.gift_direction || 'out',
        gift_person_id: d.gift_person_id || ''
      };
      peopleQuery = '';
      // 详情里已带姓名，直接记下，省得回查
      const seen = (d.participants || []).filter((p) => p.id);
      if (seen.length) {
        known = {
          ...known,
          ...Object.fromEntries(
            seen.map((p) => [p.id, { id: p.id, name: p.name, nickname: p.nickname }])
          )
        };
      }
      void remember([...form.participant_ids, form.expense_person_id, form.gift_person_id]);
    } catch (err) {
      toast.fail('读取往来失败', err);
      oncancel?.();
    } finally {
      if (seq === loadSeq) loading = false;
    }
  }

  async function remember(ids: string[]) {
    const missing = ids.filter((id) => id && !known[id]);
    if (!missing.length) return;
    const list = await resolvePeople(missing);
    known = { ...known, ...Object.fromEntries(list.map((p) => [p.id, p])) };
  }

  async function runSearch() {
    const seq = ++searchSeq;
    searching = true;
    try {
      const list = selfFirst(await searchPeople(peopleQuery.trim()));
      // 输入快过时丢掉迟到的那一次结果，免得列表闪回旧关键词
      if (seq !== searchSeq) return;
      candidates = list;
      searchFailed = false;
      known = { ...known, ...Object.fromEntries(list.map((p) => [p.id, p])) };
    } catch {
      if (seq === searchSeq) searchFailed = true;
    } finally {
      if (seq === searchSeq) searching = false;
    }
  }

  function scheduleSearch() {
    if (searchTimer) clearTimeout(searchTimer);
    searchTimer = setTimeout(runSearch, 200);
  }

  const pickedPeople = $derived(
    form.participant_ids.map((id) => known[id]).filter(Boolean) as PersonLite[]
  );
  // 开销/礼金没指定归属人时，后端会取首位参与人——把这个隐含默认显式写出来，
  // 不让用户以为「没选就是没归属」。
  const firstParticipantName = $derived(pickedPeople[0]?.name || '');
  // 已选的人不再出现在候选里，点一下就是加人，不用去列表里找已选的那个
  const unmatched = $derived(candidates.filter((p) => !form.participant_ids.includes(p.id)));

  function toggleParticipant(id: string) {
    form.participant_ids = form.participant_ids.includes(id)
      ? form.participant_ids.filter((x) => x !== id)
      : [...form.participant_ids, id];
  }

  // ===== 货殖账单关联 =====
  // 关联即时生效（不需要先保存表单）：它是一个独立动作，藏在「保存」后面会让人
  // 勾完以为挂上了、其实没提交。服务端落库时用的是货殖回的金额，不信任前端。

  async function linkBills(ids: string[]) {
    if (!editId || ids.length === 0) return;
    billBusy = true;
    try {
      const res = await API.post<{ bills: EventBill[]; missing: string[] }>(
        `/api/v1/events/${editId}/bills`,
        { remote_ids: ids }
      );
      bills = res.bills || [];
      showBillPicker = false;
      if (res.missing?.length) {
        toast.error(`${res.missing.length} 张账单在货殖里找不到（可能已删除），其余已关联`);
      } else {
        toast.ok(`已关联 ${ids.length} 张账单`);
      }
    } catch (err) {
      toast.fail('关联账单失败', err);
    } finally {
      billBusy = false;
    }
  }

  async function unlinkBill(id: string) {
    if (!editId) return;
    billBusy = true;
    try {
      bills = await API.delete<EventBill[]>(`/api/v1/events/${editId}/bills/${id}`);
      toast.ok('已解除关联');
    } catch (err) {
      toast.fail('解除关联失败', err);
    } finally {
      billBusy = false;
    }
  }

  async function refreshBills() {
    if (!editId) return;
    billBusy = true;
    try {
      const res = await API.post<{ bills: EventBill[]; stale: string[] }>(
        `/api/v1/events/${editId}/bills/refresh`,
        {}
      );
      bills = res.bills || [];
      if (res.stale?.length) {
        toast.error(`${res.stale.length} 张账单在货殖里已不存在，请检查后解除关联`);
      } else {
        toast.ok('已按货殖最新数据刷新');
      }
    } catch (err) {
      toast.fail('刷新账单失败', err);
    } finally {
      billBusy = false;
    }
  }

  // 常见场景的一键预填：只填「骨架」，明细仍由用户补
  const TEMPLATES = [
    { name: '见面聊天', title: '见面', summary: '聊了聊近况', has_gift: false },
    { name: '一起吃饭', title: '吃饭', summary: '', has_gift: false },
    { name: '送礼', title: '送礼', summary: '', has_gift: true, gift: '' },
    { name: '电话/微信', title: '通话', summary: '', has_gift: false },
    { name: '运动出游', title: '一起运动', summary: '', has_gift: false }
  ];

  function applyTemplate(t: (typeof TEMPLATES)[number]) {
    form = {
      ...form,
      title: t.title,
      summary: form.summary || t.summary,
      has_gift: t.has_gift,
      gift: t.has_gift ? form.gift || t.gift || '' : ''
    };
  }

  async function submit() {
    if (!form.title.trim() || !form.event_date) {
      toast.error('标题和日期必填');
      return;
    }
    const locations = form.locations.map((s) => s.trim()).filter(Boolean);
    const body: any = {
      title: form.title,
      event_date: form.event_date,
      locations,
      has_gift: form.has_gift,
      gift: form.has_gift ? form.gift.trim() : '',
      summary: form.summary,
      participant_ids: form.participant_ids
    };
    if (form.type_id) body.type_id = form.type_id;
    const expenseFen = toFen(form.expense_yuan);
    if (hasBills) {
      // 开销以关联账单为准：这里既不上报手填金额、也不上报 0。
      // 上报 0 会让 store 顺手删掉那笔历史账目，而它随时可能因为解除关联
      // 重新生效——账单只是接了显示权，没资格删账。
    } else if (expenseFen > 0) {
      body.expense_fen = expenseFen;
      if (form.expense_person_id) body.expense_person_id = form.expense_person_id;
      else if (form.participant_ids.length === 0) {
        toast.error('填写开销时需要选择参与人或指定开销归属人');
        return;
      }
    } else if (editId) {
      body.expense_fen = 0; // 清空原有开销
    }
    if (giftFen > 0) {
      body.gift_amount_fen = giftFen;
      body.gift_direction = form.gift_direction === 'in' ? 'in' : 'out';
      if (form.gift_person_id) body.gift_person_id = form.gift_person_id;
      else if (form.participant_ids.length === 0) {
        toast.error('填写礼金时需要选择参与人或指定礼金归属人');
        return;
      }
    } else if (editId) {
      body.gift_amount_fen = 0; // 清空原有礼金（store 会连带删掉那笔账）
    }
    saving = true;
    const isEdit = !!editId;
    let saved = false;
    try {
      if (isEdit) await API.put(`/api/v1/events/${editId}`, body);
      else await API.post('/api/v1/events', body);
      saved = true;
    } catch (err) {
      // 失败时不回调 onsave：父级因此不会关弹窗，用户填的内容原样留着
      toast.fail('保存失败', err);
    } finally {
      saving = false;
    }
    if (!saved) return;
    toast.ok(isEdit ? '已更新' : '已保存');
    onsave?.();
  }
</script>

<div class="q-stack" class:opacity-60={loading}>
  {#if loading}
    <div class="py-6 text-center text-sm" style="color: var(--q-muted);">加载中…</div>
  {:else}
    {#if !editId}
      <div class="flex flex-wrap gap-1.5">
        {#each TEMPLATES as t (t.name)}
          <button class="q-chip" onclick={() => applyTemplate(t)}>{t.name}</button>
        {/each}
      </div>
    {/if}
    <input class="q-input" placeholder="标题" bind:value={form.title} />
    <div class="grid grid-cols-2 gap-2">
      <select bind:value={form.type_id} class="q-select">
        <option value={0}>不选类型</option>
        {#each typeList as t (t.id)}<option value={t.id}>{t.name}</option>{/each}
      </select>
      <input type="date" class="q-input" bind:value={form.event_date} />
    </div>

    <div class="q-stack--tight flex flex-col">
      <div class="q-label !mb-0">地点（可多个）</div>
      {#each form.locations as _, i (i)}
        <div class="flex items-center gap-2">
          <input
            class="q-input"
            placeholder={i === 0 ? '地点，如 茶馆' : '追加地点'}
            bind:value={form.locations[i]}
          />
          {#if form.locations.length > 1}
            <button
              class="q-btn q-btn--subtle q-btn--sm shrink-0"
              onclick={() => (form.locations = form.locations.filter((_, j) => j !== i))}
              >移除</button
            >
          {/if}
        </div>
      {/each}
      <button
        class="q-btn q-btn--ghost q-btn--sm w-fit"
        onclick={() => (form.locations = [...form.locations, ''])}>+ 添加地点</button
      >
    </div>

    <div class="flex items-center gap-2">
      <label class="flex shrink-0 cursor-pointer items-center gap-1.5 text-sm">
        <input type="checkbox" bind:checked={form.has_gift} /> 有礼物
      </label>
      {#if form.has_gift}
        <input class="q-input" placeholder="礼物说明，如 送了两盒茶" bind:value={form.gift} />
      {/if}
    </div>

    {#if hasBills}
      <!-- 有账单时手填金额让位：一个数字只能有一个来源，否则卡片上会出现两套花费 -->
      <div class="q-well flex items-center justify-between !py-2.5 text-sm">
        <span>开销 <span class="q-num q-money font-semibold">¥{yuan(billsSum)}</span></span>
        <span class="text-xs" style="color: var(--q-muted);">
          由 {bills.length} 张货殖账单汇总
        </span>
      </div>
    {:else}
      <div class="grid grid-cols-2 gap-2">
        <input
          type="number"
          step="0.01"
          min="0"
          class="q-input"
          placeholder="开销（元，可空）"
          bind:value={form.expense_yuan}
        />
        <PersonPicker
          bind:value={form.expense_person_id}
          selectedName={known[form.expense_person_id]?.name || ''}
          prefer={pickedPeople}
          placeholder={firstParticipantName
            ? `开销归属 · 默认 ${firstParticipantName}`
            : '开销归属（需指定）'}
        />
      </div>
    {/if}

    <div class="q-stack--tight flex flex-col">
      <div class="flex items-center justify-between">
        <div class="q-label !mb-0 flex items-center gap-1">
          <Link2 size={12} /> 货殖账单{#if bills.length > 0}（{bills.length}）{/if}
        </div>
        {#if editId}
          <div class="flex items-center gap-1">
            {#if bills.length > 0}
              <button
                type="button"
                class="q-btn q-btn--ghost q-btn--sm"
                disabled={billBusy}
                onclick={refreshBills}>刷新</button
              >
            {/if}
            <button
              type="button"
              class="q-btn q-btn--primary q-btn--sm"
              disabled={billBusy}
              onclick={() => (showBillPicker = true)}>+ 关联账单</button
            >
          </div>
        {/if}
      </div>
      {#if bills.length === 0}
        <p class="q-hint !mt-0">
          {editId
            ? '关联货殖里已有的账单后，这次往来的开销按账单汇总；不关联就用手填金额。'
            : '保存后可以关联货殖账单，把这次的花费按账单汇总。'}
        </p>
      {:else}
        <ul class="q-stack--tight flex flex-col">
          {#each bills as b (b.id)}
            <li class="q-well q-num flex items-center gap-2 !py-1.5 text-xs">
              <span class="shrink-0" style="color: var(--q-muted);">{b.tx_date || '—'}</span>
              <span class="min-w-0 flex-1 truncate">
                {b.description || '（无备注）'}{#if b.category_name}<span
                    style="color: var(--q-muted);"
                  >
                    · {b.category_name}</span
                  >{/if}
              </span>
              <span class="q-money shrink-0">¥{yuan(b.amount_fen)}</span>
              {#if b.remote_base}
                <a
                  href={`${b.remote_base}/transactions/edit/${b.remote_id}`}
                  target="_blank"
                  rel="noreferrer"
                  class="q-iconbtn !h-6 !w-6 shrink-0"
                  title="在货殖里打开"><ExternalLink size={12} /></a
                >
              {/if}
              <button
                type="button"
                class="q-iconbtn !h-6 !w-6 shrink-0"
                title="解除关联（不会删除货殖里的账单）"
                disabled={billBusy}
                onclick={() => unlinkBill(b.id)}><X size={12} /></button
              >
            </li>
          {/each}
        </ul>
      {/if}
    </div>

    <!-- 礼金：现场记完事就结账，保存时自动生成一条 kind=gift 的账目挂在这个往来上 -->
    <div class="grid grid-cols-2 gap-2">
      <input
        type="number"
        step="0.01"
        min="0"
        class="q-input"
        placeholder="礼金（元，可空）"
        bind:value={form.gift_yuan}
      />
      <select bind:value={form.gift_direction} class="q-select">
        <option value="out">随出去（我给的）</option>
        <option value="in">收到（别人给的）</option>
      </select>
    </div>
    {#if giftFen > 0}
      <PersonPicker
        bind:value={form.gift_person_id}
        selectedName={known[form.gift_person_id]?.name || ''}
        prefer={pickedPeople}
        placeholder={firstParticipantName
          ? `${form.gift_direction === 'in' ? '谁给的礼金' : '随给谁'} · 默认 ${firstParticipantName}`
          : `${form.gift_direction === 'in' ? '谁给的礼金' : '随给谁'}（需指定）`}
      />
    {/if}

    <textarea
      class="q-textarea"
      style="min-height: 80px;"
      placeholder="备注"
      bind:value={form.summary}></textarea>
    <div>
      <div class="mb-1.5 flex items-center justify-between">
        <div class="q-label !mb-0">
          参与人（已选 {form.participant_ids.length} 人）
        </div>
        <div class="relative">
          <Search
            size={12}
            class="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2"
            style="color: var(--q-faint);"
          />
          <input
            bind:value={peopleQuery}
            oninput={scheduleSearch}
            placeholder="搜索联系人"
            class="q-input w-36 !py-1.5 pl-7 text-xs"
          />
        </div>
      </div>
      {#if pickedPeople.length > 0}
        <div class="mb-1.5 flex flex-wrap gap-1.5">
          {#each pickedPeople as p (p.id)}
            <button class="q-chip" aria-pressed="true" onclick={() => toggleParticipant(p.id)}>
              {personLabel(p)} ×
            </button>
          {/each}
        </div>
      {/if}
      <div class="flex max-h-40 flex-wrap gap-1.5 overflow-auto">
        {#each unmatched as p (p.id)}
          <button
            class="q-chip"
            style={isSelf(p.id)
              ? 'color: var(--q-warn); border-color: color-mix(in srgb, var(--q-warn) 55%, var(--q-border));'
              : ''}
            onclick={() => toggleParticipant(p.id)}>{personLabel(p)}</button
          >
        {:else}
          <span
            class="text-xs"
            style={searchFailed ? 'color: var(--q-danger);' : 'color: var(--q-muted);'}
          >
            {searching ? '搜索中…' : searchFailed ? '搜索失败，请重试' : '没有匹配的联系人'}
          </span>
        {/each}
      </div>
    </div>
  {/if}
</div>

<div class="mt-4 flex items-center justify-between gap-2">
  {#if editId && onremove}
    <button
      class="q-btn q-btn--danger-ghost"
      onclick={() => onremove?.(editId, form.title, originExpenseFen)}
    >
      <Trash2 size={14} /> 删除
    </button>
  {:else}
    <span></span>
  {/if}
  <div class="flex gap-2">
    <button class="q-btn q-btn--ghost" onclick={() => oncancel?.()}>取消</button>
    <button class="q-btn q-btn--primary" disabled={saving || loading} onclick={submit}>
      {saving ? '保存中…' : '保存'}
    </button>
  </div>
</div>

{#if showBillPicker && editId}
  <BillPicker
    linkedIds={bills.map((b) => b.remote_id)}
    onclose={() => (showBillPicker = false)}
    onpick={(ids) => void linkBills(ids)}
  />
{/if}
