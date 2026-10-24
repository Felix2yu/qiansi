<script lang="ts">
  import { onMount } from 'svelte';
  import { API, todayLocal, REF_TYPE_LABEL, type Reminder } from '../lib/api';
  import { loadSelf } from '../lib/self.svelte';
  import PersonPicker from '../lib/PersonPicker.svelte';
  import Modal from '../lib/ui/Modal.svelte';
  import PageHeader from '../lib/ui/PageHeader.svelte';
  import Empty from '../lib/ui/Empty.svelte';
  import LoadError from '../lib/ui/LoadError.svelte';
  import { Plus, Check, Undo2, Bell } from '@lucide/svelte';
  import { toast, errText } from '../lib/toast.svelte';
  import { ask } from '../lib/ask.svelte';
  import { RECOVER_NOTE, trashOne } from '../lib/trash';
  import { completeReminder } from '../lib/reminderActions';

  let list = $state<Reminder[]>([]);
  // 加载失败与「没有待办」必须分开显示：前者是没读到，后者是真的没有
  let loadError = $state('');
  let showDone = $state(false);
  let showForm = $state(false);
  let editId = $state('');
  let form = $state(emptyForm());
  // 编辑时把待办自带的名字交给选人控件显示，省一次回查
  let pickedName = $state('');

  function emptyForm() {
    return { title: '', due_at: todayLocal(), person_id: '', status: 'pending' };
  }

  // 后端按本地时间比较 due_at，这里不能再拼 UTC 的 Z，否则会整体偏移一天
  async function load() {
    try {
      list = await API.get<Reminder[]>(
        `/api/v1/reminders?status=${showDone ? 'done' : 'pending'}&limit=200`
      );
      loadError = '';
    } catch (err) {
      loadError = errText(err);
      toast.fail('加载失败', err);
    }
  }
  onMount(() => {
    loadSelf();
    load();
  });

  function openCreate() {
    editId = '';
    form = emptyForm();
    pickedName = '';
    showForm = true;
  }

  // 派生待办（纪念日、承诺、联系节奏）不在 reminders 表里，id 是合成串：
  // 改与删都要回到各自的来源页，否则 PUT/DELETE 打在不存在的行上假装成功。
  function derivedNotice(r: Reminder, action: '改' | '删'): string {
    if (r.id.startsWith('anniv:')) return `纪念日自动生成的提醒请到「纪念日」里${action}`;
    if (r.id.startsWith('promise:')) return `到期承诺自动生成的待办请到「对话」里${action}`;
    if (r.id.startsWith('contact:'))
      return action === '改'
        ? '这条按联系节奏自动生成，要改节奏请去「设置 · 联系节奏」'
        : '这条按联系节奏自动生成，勾掉（记一次「联系过了」）它就不再催';
    return '';
  }

  function openEdit(r: Reminder) {
    const notice = derivedNotice(r, '改');
    if (notice) {
      toast.info(notice);
      return;
    }
    editId = r.id;
    pickedName = r.person_name || '';
    form = {
      title: r.title,
      due_at: (r.due_at || '').slice(0, 10),
      person_id: r.person_id || '',
      status: r.status || 'pending'
    };
    showForm = true;
  }

  function closeForm() {
    showForm = false;
    editId = '';
  }

  async function submit() {
    if (!form.title.trim() || !form.due_at) {
      toast.error('内容和日期必填');
      return;
    }
    const body: any = { ...form, due_at: form.due_at + 'T09:00:00' };
    if (!body.person_id) delete body.person_id;
    const isEdit = !!editId;
    try {
      if (isEdit) await API.put(`/api/v1/reminders/${editId}`, body);
      else await API.post('/api/v1/reminders', body);
    } catch (err) {
      toast.fail('保存失败', err);
      return;
    }
    closeForm();
    toast.ok(isEdit ? '已更新' : '已添加待办');
    await load();
  }

  // 按时间分区：逾期 / 今天 / 未来，避免逾期项与近期项混排后失去优先级
  const today = $derived(todayLocal());
  const groups = $derived.by(() => {
    if (showDone) return [{ key: 'done', label: '已完成', items: list }];
    const overdue: Reminder[] = [],
      dueToday: Reminder[] = [],
      upcoming: Reminder[] = [];
    for (const r of list) {
      const d = (r.due_at || '').slice(0, 10);
      if (d && d < today) overdue.push(r);
      else if (d === today) dueToday.push(r);
      else upcoming.push(r);
    }
    return [
      { key: 'overdue', label: '已逾期', items: overdue },
      { key: 'today', label: '今天', items: dueToday },
      { key: 'upcoming', label: '之后', items: upcoming }
    ].filter((g) => g.items.length > 0);
  });

  function overdueDays(r: Reminder) {
    const d = (r.due_at || '').slice(0, 10);
    if (!d) return 0;
    const diff = Math.floor((new Date(today).getTime() - new Date(d).getTime()) / 86400000);
    return diff > 0 ? diff : 0;
  }

  // PUT 是全量更新，必须带上原对象的其他字段，否则会被置空
  function restore(r: Reminder) {
    return API.put(`/api/v1/reminders/${r.id}`, { ...r, status: 'pending', completed_at: '' });
  }

  // 派生待办的「完成」写的是来源记录（承诺兑现 / 关闭本次提醒 / 联系打卡），
  // 这里没有对应的反向接口，所以不给撤销按钮，只指路
  function isDerived(r: Reminder) {
    return r.id.startsWith('anniv:') || r.id.startsWith('promise:') || r.id.startsWith('contact:');
  }

  async function done(r: Reminder) {
    await completeReminder(r, load);
  }

  async function undo(r: Reminder) {
    try {
      await restore(r);
      toast.ok('已改为未完成');
      await load();
    } catch (err) {
      toast.fail('操作失败', err);
    }
  }

  async function remove(r: Reminder) {
    const notice = derivedNotice(r, '删');
    if (notice) {
      toast.info(notice);
      return;
    }
    if (
      !(await ask({
        title: '删除这条待办？',
        detail: RECOVER_NOTE,
        danger: true,
        confirmLabel: '删除'
      }))
    )
      return;
    await trashOne('reminder', r.id, load, '已删除');
  }
</script>

<div class="q-page">
  <PageHeader title="待办" subtitle="自定义 & 自动生成">
    {#snippet actions()}
      <button class="q-btn q-btn--primary q-btn--sm" onclick={openCreate}>
        <Plus size={14} /> 新建
      </button>
    {/snippet}
  </PageHeader>

  <div class="q-stack">
    <div class="q-card q-card--tight flex items-center gap-2">
      <label class="flex cursor-pointer items-center gap-2 text-sm select-none">
        <input type="checkbox" bind:checked={showDone} onchange={() => load()} /> 查看已完成
      </label>
    </div>

    {#if groups.length === 0}
      {#if loadError}
        <LoadError text={`加载失败：${loadError}`} onretry={() => load()} />
      {:else}
        <Empty icon={Bell} text={showDone ? '还没有已完成的待办' : '没有待办，正好去见见想见的人'} />
      {/if}
    {:else}
      {#each groups as g, i (i)}
        <section>
          <h2
            class="mb-2 flex items-center gap-2 text-xs font-medium tracking-widest"
            style={`color: ${g.key === 'overdue' ? 'var(--q-danger)' : 'var(--q-muted)'};`}
          >
            {g.label}
            <span class="q-badge !text-[10px]">{g.items.length}</span>
          </h2>
          <div class="q-stack--tight flex flex-col">
            {#each g.items as r (r.id)}
              <div class="q-row">
                {#if r.status === 'pending'}
                  <button
                    class="q-iconbtn q-iconbtn--border !h-6 !w-7 shrink-0 rounded-md"
                    onclick={() => done(r)}
                    title="标记完成"
                  >
                    <Check size={14} />
                  </button>
                {:else if isDerived(r)}
                  <!-- 派生待办完成了就是来源记录定了论，这里点不回，按钮留着只会报错 -->
                  <span
                    class="flex h-6 w-6 shrink-0 items-center justify-center rounded-md"
                    style="color: var(--q-ok);"><Check size={15} /></span
                  >
                {:else}
                  <button
                    class="q-iconbtn q-iconbtn--border !h-6 !w-7 shrink-0 rounded-md"
                    onclick={() => undo(r)}
                    title="撤销完成"
                  >
                    <Undo2 size={13} />
                  </button>
                {/if}
                <div class="min-w-0 flex-1">
                  <div
                    class="text-sm"
                    class:line-through={r.status !== 'pending'}
                    class:opacity-60={r.status !== 'pending'}
                  >
                    {r.title}
                  </div>
                  <div class="q-num mt-0.5 text-xs" style="color: var(--q-muted);">
                    {REF_TYPE_LABEL[r.ref_type] || r.ref_type}{r.person_name &&
                      ' · ' + r.person_name}
                    · {new Date(r.due_at).toLocaleDateString()}
                    {#if g.key === 'overdue'}<span style="color: var(--q-danger);">
                        · 已逾期 {overdueDays(r)} 天</span
                      >{/if}
                  </div>
                </div>
                <button class="q-btn q-btn--ghost q-btn--sm shrink-0" onclick={() => openEdit(r)}
                  >编辑</button
                >
                <button class="q-btn q-btn--subtle q-btn--sm shrink-0" onclick={() => remove(r)}
                  >删除</button
                >
              </div>
            {/each}
          </div>
        </section>
      {/each}
    {/if}
  </div>
</div>

{#if showForm}
  <Modal wide onclose={closeForm} title={editId ? '编辑待办' : '新建待办'}>
    <div class="q-stack--tight flex flex-col">
      <input bind:value={form.title} class="q-input" placeholder="待办内容" />
      <input type="date" bind:value={form.due_at} class="q-input" />
      <PersonPicker
        bind:value={form.person_id}
        selectedName={pickedName}
        placeholder="关联联系人（可选）"
      />
    </div>
    {#snippet foot()}
      <button class="q-btn q-btn--ghost" onclick={closeForm}>取消</button>
      <button class="q-btn q-btn--primary" onclick={submit}>保存</button>
    {/snippet}
  </Modal>
{/if}
