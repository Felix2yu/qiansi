<script lang="ts">
  import { onMount } from 'svelte';
  import { API, todayLocal, type Anniversary } from '../lib/api';
  import { loadSelf } from '../lib/self.svelte';
  import PersonPicker from '../lib/PersonPicker.svelte';
  import Modal from '../lib/ui/Modal.svelte';
  import PageHeader from '../lib/ui/PageHeader.svelte';
  import Empty from '../lib/ui/Empty.svelte';
  import { toast } from '../lib/toast.svelte';
  import { ask } from '../lib/ask.svelte';
  import { RECOVER_NOTE, trashOne } from '../lib/trash';
  import { Plus, Cake } from '@lucide/svelte';

  let list = $state<Anniversary[]>([]);
  let showForm = $state(false);
  let editId = $state('');
  let form = $state(emptyForm());
  // 编辑时把纪念日自带的名字交给选人控件显示，省一次回查
  let pickedName = $state('');

  function emptyForm() {
    return {
      person_id: '',
      title: '',
      date: todayLocal(),
      is_lunar: false,
      repeat_yearly: true,
      remind_days: '7,3,1,0'
    };
  }

  async function load() {
    list =
      (await API.get<Anniversary[]>('/api/v1/anniversaries').catch((err: any) => {
        // 读失败也不能装作「暂无」：空列表和查不到是两件事
        toast.fail('加载失败', err);
        return [] as Anniversary[];
      })) || [];
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

  function openEdit(a: Anniversary) {
    editId = a.id;
    pickedName = a.person_name || '';
    form = {
      person_id: a.person_id || '',
      title: a.title,
      date: (a.date || '').slice(0, 10),
      is_lunar: !!a.is_lunar,
      repeat_yearly: !!a.repeat_yearly,
      remind_days: a.remind_days || '7,3,1,0'
    };
    showForm = true;
  }

  function closeForm() {
    showForm = false;
    editId = '';
  }

  async function submit() {
    if (!form.title.trim() || !form.date) {
      toast.error('标题和日期必填');
      return;
    }
    const body: any = { ...form };
    if (!body.person_id) delete body.person_id;
    if (!body.remind_days.trim()) body.remind_days = '0';
    const isEdit = !!editId;
    try {
      if (isEdit) await API.put(`/api/v1/anniversaries/${editId}`, body);
      else await API.post('/api/v1/anniversaries', body);
    } catch (err) {
      // 失败时弹窗不关、内容不丢，改完可以直接再存一次
      toast.fail('保存失败', err);
      return;
    }
    closeForm();
    toast.ok(isEdit ? '已更新' : '已保存');
    await load();
  }

  // 删除只进回收站：撤销窗口过后仍能在回收站请回来
  async function remove(a: Anniversary) {
    if (
      !(await ask({
        title: `删除「${a.title}」？`,
        detail: RECOVER_NOTE,
        danger: true,
        confirmLabel: '删除'
      }))
    )
      return;
    await trashOne('anniversary', a.id, load, '已删除');
  }

  /** 每年重复的纪念日只有月日有意义，展示时补上今年的年份便于阅读 */
  function displayDate(a: Anniversary) {
    const d = new Date(a.date);
    return a.repeat_yearly ? `${d.getMonth() + 1}月${d.getDate()}日` : d.toLocaleDateString();
  }

  /** 角标显示下一次发生的月日（循环/农历项取后端换算后的公历日期） */
  function badgeDate(a: Anniversary) {
    const d = new Date(a.next_date || a.date);
    return { m: d.getMonth() + 1, d: d.getDate() };
  }

  /** 倒计时文案；null 表示不展示 chip */
  function countdownLabel(a: Anniversary): string | null {
    if (a.days_until == null) return a.repeat_yearly ? null : '已过';
    if (a.days_until === 0) return '今天';
    return `${a.days_until} 天`;
  }
</script>

<div class="q-page">
  <PageHeader title="纪念日" subtitle="生日、周年、倒数日">
    {#snippet actions()}
      <button class="q-btn q-btn--primary q-btn--sm" onclick={openCreate}>
        <Plus size={14} /> 新建
      </button>
    {/snippet}
  </PageHeader>

  <div class="q-stack--tight flex flex-col">
    {#each list as a (a.id)}
      {@const bd = badgeDate(a)}
      {@const cd = countdownLabel(a)}
      {@const soon = a.days_until != null && a.days_until <= 7}
      <div class="q-row">
        <div
          class="q-num flex h-11 w-11 shrink-0 flex-col items-center justify-center rounded-[10px]"
          style="background: var(--q-theme); color: var(--q-on-theme);"
        >
          <div class="text-[10px] leading-none opacity-85">{bd.m}月</div>
          <div class="q-num mt-0.5 text-base leading-none font-bold">{bd.d}</div>
        </div>
        <div class="min-w-0 flex-1">
          <div class="truncate text-sm font-medium">{a.title}</div>
          <div class="q-num mt-0.5 truncate text-xs" style="color: var(--q-muted);">
            {a.person_name && `${a.person_name} · `}
            {a.is_lunar ? '农历 ' : ''}{displayDate(a)}
            {a.repeat_yearly ? ' · 每年循环' : ''}
          </div>
        </div>
        {#if cd}
          <span class="q-badge shrink-0" class:q-badge--theme={soon}>{cd}</span>
        {/if}
        <button class="q-btn q-btn--ghost q-btn--sm shrink-0" onclick={() => openEdit(a)}
          >编辑</button
        >
        <button class="q-btn q-btn--subtle q-btn--sm shrink-0" onclick={() => remove(a)}
          >删除</button
        >
      </div>
    {:else}
      <Empty icon={Cake} text="还没有纪念日，把生日和重要日子记下来" />
    {/each}
  </div>
</div>

{#if showForm}
  <Modal wide onclose={closeForm} title={editId ? '编辑纪念日' : '新建纪念日'}>
    <div class="q-stack--tight flex flex-col">
      <PersonPicker
        bind:value={form.person_id}
        selectedName={pickedName}
        placeholder="自属（倒数日）"
      />
      <input bind:value={form.title} class="q-input" placeholder="标题（如：妈妈生日）" />
      <input type="date" bind:value={form.date} class="q-input" />
      <div class="flex items-center gap-5">
        <label
          class="flex cursor-pointer items-center gap-2 text-sm select-none"
          style="color: var(--q-muted);"
        >
          <input type="checkbox" bind:checked={form.is_lunar} /> 农历
        </label>
        <label
          class="flex cursor-pointer items-center gap-2 text-sm select-none"
          style="color: var(--q-muted);"
        >
          <input type="checkbox" bind:checked={form.repeat_yearly} /> 每年循环
        </label>
      </div>
      <input
        bind:value={form.remind_days}
        class="q-input"
        placeholder="提醒天数（逗号分隔，如 7,3,1,0）"
      />
    </div>
    {#snippet foot()}
      <button class="q-btn q-btn--ghost" onclick={closeForm}>取消</button>
      <button class="q-btn q-btn--primary" onclick={submit}>保存</button>
    {/snippet}
  </Modal>
{/if}
