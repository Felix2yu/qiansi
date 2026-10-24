<script lang="ts">
  import { onMount } from 'svelte';
  import { API, MEMO_STATUS_LABEL, todayLocal, type Memo } from '../lib/api';
  import { Plus, MessageCircle } from '@lucide/svelte';
  import Modal from '../lib/ui/Modal.svelte';
  import PageHeader from '../lib/ui/PageHeader.svelte';
  import Empty from '../lib/ui/Empty.svelte';
  import LoadError from '../lib/ui/LoadError.svelte';
  import { personLabel, loadSelf } from '../lib/self.svelte';
  import PersonPicker from '../lib/PersonPicker.svelte';
  import { toast, errText } from '../lib/toast.svelte';
  import { ask } from '../lib/ask.svelte';
  import { RECOVER_NOTE, trashOne } from '../lib/trash';

  let list = $state<Memo[]>([]);
  // 加载失败与「暂无记录」必须分开显示：前者是没读到，后者是真的没有
  let loadError = $state('');
  let onlyPromises = $state(false);
  let showForm = $state(false);
  let editId = $state('');
  let form = $state(emptyForm());
  // 编辑时把列表里带的名字交给选人控件显示，省一次回查
  let pickedName = $state('');

  function emptyForm() {
    return {
      person_id: '',
      speaker: 'other',
      content: '',
      said_at: todayLocal(),
      is_promise: false,
      due_date: '',
      status: 'open'
    };
  }

  const PAGE = 50;
  let page = $state(0);
  let hasMore = $state(false);

  async function load(reset = true) {
    if (reset) page = 0;
    let batch: Memo[] | null = null;
    try {
      batch = await API.get<Memo[]>(
        `/api/v1/memos?promises_only=${onlyPromises ? 1 : 0}&limit=${PAGE}&offset=${page * PAGE}`
      );
    } catch (err) {
      // 读失败也不能装作「暂无记录」：空列表和查不到是两件事
      loadError = errText(err);
      toast.fail('加载失败', err);
      return;
    }
    loadError = '';
    hasMore = batch.length === PAGE;
    list = reset ? batch : [...list, ...batch];
  }
  async function loadMore() {
    page += 1;
    await load(false);
  }
  onMount(() => {
    loadSelf();
    load(true);
  });

  function openCreate() {
    editId = '';
    form = emptyForm();
    pickedName = '';
    showForm = true;
  }

  function openEdit(m: Memo) {
    editId = m.id;
    pickedName = m.person_name || '';
    form = {
      person_id: m.person_id || '',
      speaker: m.speaker || 'other',
      content: m.content,
      said_at: (m.said_at || '').slice(0, 10),
      is_promise: !!m.is_promise,
      due_date: m.due_date ? m.due_date.slice(0, 10) : '',
      status: m.status || 'open'
    };
    showForm = true;
  }

  function closeForm() {
    showForm = false;
    editId = '';
  }

  async function submit() {
    if (!form.content.trim()) {
      toast.error('内容必填');
      return;
    }
    if (!form.said_at) form.said_at = todayLocal();
    const body: any = { ...form };
    if (!body.person_id) delete body.person_id;
    if (!body.due_date) delete body.due_date;
    const isEdit = !!editId;
    try {
      if (isEdit) await API.put(`/api/v1/memos/${editId}`, body);
      else await API.post('/api/v1/memos', body);
    } catch (err) {
      // 失败时弹窗不关、内容不丢，改完可以直接再存一次
      toast.fail('保存失败', err);
      return;
    }
    closeForm();
    toast.ok(isEdit ? '已更新' : '已保存');
    await load();
  }

  async function togglePromise(m: Memo) {
    const was = !!m.is_promise;
    const snapshot = { ...m };
    try {
      await API.put(`/api/v1/memos/${m.id}`, { ...snapshot, is_promise: !was });
      toast.undoable(was ? '已取消承诺标记' : '已标记为承诺', async () => {
        await API.put(`/api/v1/memos/${m.id}`, { ...snapshot, is_promise: was }).catch(() => {});
        await load();
      });
      await load();
    } catch (err) {
      toast.fail('保存失败', err);
    }
  }

  async function mark(m: Memo, status: string) {
    const was = m.status;
    const snapshot = { ...m };
    try {
      await API.put(`/api/v1/memos/${m.id}`, { ...snapshot, status });
      toast.undoable(`已标记为「${MEMO_STATUS_LABEL[status] || status}」`, async () => {
        await API.put(`/api/v1/memos/${m.id}`, { ...snapshot, status: was }).catch(() => {});
        await load();
      });
      await load();
    } catch (err) {
      toast.fail('保存失败', err);
    }
  }

  async function remove(m: Memo) {
    if (
      !(await ask({
        title: '删除这条对话记录？',
        detail: RECOVER_NOTE,
        danger: true,
        confirmLabel: '删除'
      }))
    )
      return;
    await trashOne('memo', m.id, load, '已删除');
  }
  function memoWho(m: Memo) {
    if (!m.person_id) return '（未关联）';
    // 名字随列表一起回来，不再为了这几个字拉全量名单
    return personLabel({ id: m.person_id, name: m.person_name || '（已删除）' });
  }
</script>

<div class="q-page">
  <PageHeader title="对话 / 承诺" subtitle="记录重要的话与约定">
    {#snippet actions()}
      <button class="q-btn q-btn--primary q-btn--sm" onclick={openCreate}>
        <Plus size={14} /> 新建
      </button>
    {/snippet}
  </PageHeader>

  <div class="q-stack">
    <div class="q-card q-card--tight flex items-center gap-2">
      <label class="flex cursor-pointer items-center gap-2 text-sm select-none">
        <input type="checkbox" bind:checked={onlyPromises} onchange={() => load()} /> 仅显示承诺
      </label>
    </div>

    <div class="q-stack--tight flex flex-col">
      {#each list as m (m.id)}
        <div class="q-card q-card--tight">
          <div class="flex items-start gap-2">
            <div class="min-w-0 flex-1 text-sm leading-relaxed">{m.content}</div>
            <button class="q-btn q-btn--ghost q-btn--sm shrink-0" onclick={() => openEdit(m)}
              >编辑</button
            >
            <button class="q-btn q-btn--subtle q-btn--sm shrink-0" onclick={() => remove(m)}
              >删除</button
            >
          </div>
          <div
            class="mt-2.5 flex flex-wrap items-center gap-x-2 gap-y-1.5 text-xs"
            style="color: var(--q-muted);"
          >
            {#if m.is_promise}<span class="q-badge q-badge--warn">承诺</span>{/if}
            {#if m.status === 'open' && m.is_promise}
              <button
                class="link-btn"
                style="color: var(--q-ok);"
                onclick={() => mark(m, 'fulfilled')}>兑现</button
              >
              <button
                class="link-btn"
                style="color: var(--q-danger);"
                onclick={() => mark(m, 'broken')}>未兑现</button
              >
            {/if}
            {#if m.status !== 'open'}
              <span
                class="q-badge"
                class:q-badge--ok={m.status === 'fulfilled'}
                class:q-badge--danger={m.status === 'broken'}
                >{MEMO_STATUS_LABEL[m.status] || m.status}</span
              >
              <!-- 兑现/未兑现已定论后仍要能改回来：否则一次误点就永久盖住了这条承诺 -->
              <button class="link-btn" onclick={() => mark(m, 'open')}>改回待办</button>
            {/if}
            <span class="q-num"
              >{memoWho(m)} · {m.speaker === 'me' ? '我说' : '对方说'} ·
              {new Date(m.said_at).toLocaleDateString()}</span
            >
            {#if m.due_date}<span class="q-num"
                >到期 {new Date(m.due_date).toLocaleDateString()}</span
              >{/if}
            <button class="link-btn ml-auto" onclick={() => togglePromise(m)}>
              {m.is_promise ? '取消承诺' : '标记承诺'}
            </button>
          </div>
        </div>
      {:else}
        {#if loadError}
          <LoadError text={`加载失败：${loadError}`} onretry={() => load()} />
        {:else}
          <Empty
            icon={MessageCircle}
            text={onlyPromises ? '还没有承诺记录' : '暂无记录，把重要的话记下来'}
          />
        {/if}
      {/each}
    </div>

    {#if loadError && list.length > 0}
      <div class="text-center text-sm" style="color: var(--q-danger);">
        加载失败：{loadError}
        <button class="q-btn q-btn--ghost q-btn--sm" onclick={() => load()}>重试</button>
      </div>
    {/if}

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
  <Modal wide onclose={closeForm} title={editId ? '编辑对话' : '新建对话'}>
    <div class="q-stack--tight flex flex-col">
      <PersonPicker
        bind:value={form.person_id}
        selectedName={pickedName}
        placeholder="选择联系人（可选）"
      />
      <div class="grid grid-cols-2 gap-2">
        <select bind:value={form.speaker} class="q-select">
          <option value="other">对方说</option>
          <option value="me">我说</option>
        </select>
        <input type="date" bind:value={form.said_at} class="q-input" />
      </div>
      <textarea
        class="q-textarea"
        style="min-height: 110px;"
        placeholder="对话 / 内容"
        bind:value={form.content}></textarea>
      <label
        class="flex cursor-pointer items-center gap-2 text-sm select-none"
        style="color: var(--q-muted);"
      >
        <input type="checkbox" bind:checked={form.is_promise} /> 这是一个承诺（可设置到期）
      </label>
      {#if form.is_promise}
        <input
          type="date"
          bind:value={form.due_date}
          class="q-input"
          placeholder="到期日期（可选）"
        />
      {/if}
    </div>
    {#snippet foot()}
      <button class="q-btn q-btn--ghost" onclick={closeForm}>取消</button>
      <button class="q-btn q-btn--primary" onclick={submit}>保存</button>
    {/snippet}
  </Modal>
{/if}
