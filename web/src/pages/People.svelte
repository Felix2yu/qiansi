<script lang="ts">
  import { onMount } from 'svelte';
  import {
    API,
    avatarBg,
    genderLabel,
    GENDER_OPTIONS,
    yuanShort,
    netLabel,
    contactAgo,
    type Person
  } from '../lib/api';
  import PersonForm from '../lib/PersonForm.svelte';
  import Modal from '../lib/ui/Modal.svelte';
  import PageHeader from '../lib/ui/PageHeader.svelte';
  import Empty from '../lib/ui/Empty.svelte';
  import { navigate } from '../lib/router';
  import { Search, Plus, Trash2, Upload, Download, Undo2, Pencil, Users } from '@lucide/svelte';
  import { dict, ensure } from '../lib/dict.svelte';
  import { toast } from '../lib/toast.svelte';
  import { ask } from '../lib/ask.svelte';
  import { RECOVER_NOTE, trashOne } from '../lib/trash';

  let { mode = 'list' }: { mode?: string } = $props();

  let list = $state<Person[]>([]);
  let q = $state('');
  const categories = $derived(dict.categories);
  const tags = $derived(dict.tags);
  let selectedCat = $state<number>(0);
  let selectedTag = $state<number>(0);
  // 性别筛选：M / F / none（未填），空串是不限
  let selectedGender = $state('');
  // 归档的人默认不在列表里，靠这个筛选翻出来找回
  let onlyArchived = $state(false);
  function toggleArchived() {
    onlyArchived = !onlyArchived;
    selectMode = false;
    selectedIds = [];
    load();
  }
  let showForm = $state(false);
  let editing = $state<Person | null>(null);
  let importing = $state(false);
  let vcardInput: HTMLInputElement | undefined = $state();

  // 批量选择态
  let selectMode = $state(false);
  let selectedIds = $state<string[]>([]);
  function toggleSelect(id: string) {
    selectedIds = selectedIds.includes(id)
      ? selectedIds.filter((x) => x !== id)
      : [...selectedIds, id];
  }
  function allVisibleSelected() {
    return list.length > 0 && list.every((p) => selectedIds.includes(p.id));
  }
  function toggleAll() {
    if (allVisibleSelected()) {
      selectedIds = [];
    } else {
      selectedIds = [...new Set([...selectedIds, ...list.map((p) => p.id)])];
    }
  }
  function toggleSelectMode() {
    selectMode = !selectMode;
    if (!selectMode) selectedIds = [];
  }
  // 批量把选中的人加进一个圈子（只增不减，移出仍回单人编辑弹窗）
  let bulkCat = $state(0);
  async function addSelectedToCircle() {
    if (selectedIds.length === 0 || bulkCat === 0) return;
    const name = categories.find((c) => c.id === bulkCat)?.name || '';
    try {
      const res = await API.post<{ added: number }>('/api/v1/people/bulk-categories', {
        ids: selectedIds,
        category_ids: [bulkCat]
      });
      bulkCat = 0;
      await load();
      toast.ok(
        res.added === selectedIds.length
          ? `已把 ${res.added} 位加入「${name}」`
          : `${selectedIds.length} 位里有 ${res.added} 位新加入「${name}」，其余原本就在圈子里`
      );
    } catch (err) {
      toast.fail('加入圈子失败', err);
    }
  }
  // 批量只改一列：走 bulk-update 的白名单，不能走 PUT——那是整行覆盖，
  // 一个 body 只带性别的请求会把备注、圈子、生日全写成零值。
  type BulkField = { key: string; label: string; options: { v: string; t: string }[] | null };
  const BULK_FIELDS: BulkField[] = [
    {
      key: 'gender',
      label: '性别',
      options: [...GENDER_OPTIONS.map((o) => ({ v: o.code, t: o.label })), { v: '', t: '未填' }]
    },
    {
      key: 'grade',
      label: '亲密度',
      options: [
        ...[1, 2, 3, 4, 5].map((g) => ({ v: String(g), t: '♥'.repeat(g) })),
        { v: '0', t: '未设置' }
      ]
    },
    { key: 'location', label: '位置', options: null }
  ];
  let bulkField = $state('gender');
  let bulkValue = $state('M');
  let bulkOnlyEmpty = $state(true);
  let busyBulk = $state(false);
  let bulkDef = $derived(BULK_FIELDS.find((f) => f.key === bulkField)!);
  function resetBulkValue() {
    bulkValue = bulkDef.options?.[0]?.v ?? '';
  }
  async function applyBulkField() {
    if (selectedIds.length === 0) return;
    busyBulk = true;
    try {
      const value = bulkField === 'grade' ? Number(bulkValue) : bulkValue;
      const res = await API.post<{ updated: number }>('/api/v1/people/bulk-update', {
        ids: selectedIds,
        set: { [bulkField]: value },
        only_empty: bulkOnlyEmpty
      });
      await load();
      toast.ok(
        res.updated === 0 && bulkOnlyEmpty
          ? `选中的 ${selectedIds.length} 位都填过${bulkDef.label}了，没有改动`
          : `已更新 ${res.updated} 位的${bulkDef.label}`
      );
    } catch (err) {
      toast.fail('批量更新失败', err);
    } finally {
      busyBulk = false;
    }
  }
  // 危险操作只留一个确认框：要手打确认词太磨人，误触的代价由回收站来兜。
  // 批量删除是软删除，可逐个从回收站找回，所以不再强调「不可恢复」，
  // 但仍报明会连带哪些记录——让人知道删的不止这几个人。
  async function askDeleteSelected() {
    const ids = [...selectedIds];
    if (ids.length === 0) return;
    if (
      !(await ask({
        title: `删除选中的 ${ids.length} 位联系人？`,
        detail: `他们的往来、对话、记账与纪念日会一并进回收站，可在回收站逐个找回。`,
        danger: true,
        confirmLabel: `删除 ${ids.length} 位`
      }))
    )
      return;
    await deleteSelected(ids);
  }
  async function deleteSelected(ids: string[]) {
    try {
      const res = await API.delete<{ deleted: number }>('/api/v1/people', { ids });
      selectedIds = [];
      await load();
      toast.ok(`已删除 ${res.deleted} 位联系人，可在回收站找回`);
    } catch (err) {
      toast.fail('删除失败', err);
    }
  }
  async function askClearAll() {
    if (
      !(await ask({
        title: '清空全部联系人？',
        detail:
          '所有往来、对话、记账与纪念日会一并进回收站。回收站里的记录恢复前不会出现在任何列表里，彻底清空才真的删除。',
        danger: true,
        confirmLabel: '清空全部'
      }))
    )
      return;
    await clearAll();
  }
  async function clearAll() {
    try {
      const res = await API.delete<{ deleted: number }>('/api/v1/people', { ids: [] });
      selectedIds = [];
      await load();
      toast.ok(`已清空 ${res.deleted} 位联系人，可在回收站找回`);
    } catch (err) {
      toast.fail('清空失败', err);
    }
  }

  // 分页：一次取一页，避免数据量上来后被静默截断
  const PAGE = 60;
  let page = $state(0);
  let hasMore = $state(false);

  async function load(reset = true) {
    if (reset) page = 0;
    const archived = onlyArchived ? '&archived=only' : '';
    const gender = selectedGender ? `&gender=${selectedGender}` : '';
    const batch = await API.get<Person[]>(
      `/api/v1/people?q=${encodeURIComponent(q)}${archived}${gender}&category_id=${selectedCat}&tag_id=${selectedTag}&limit=${PAGE}&offset=${page * PAGE}`
    ).catch((err) => {
      // 读失败也不能显示「暂无联系人」，那看起来像是数据没了
      toast.fail('加载失败', err);
      return [] as Person[];
    });
    hasMore = batch.length === PAGE;
    list = reset ? batch : [...list, ...batch];
  }
  async function loadMore() {
    page += 1;
    await load(false);
  }
  async function init() {
    ensure('categories');
    ensure('tags');
    await load();
    if (mode === 'new') {
      openCreate();
    }
  }
  onMount(init);

  function openCreate() {
    editing = null;
    showForm = true;
  }
  function openEdit(p: Person) {
    editing = p;
    showForm = true;
  }
  async function onSaved(saved: Person) {
    showForm = false;
    await load();
    if (mode === 'new' && saved?.id) {
      navigate(`/people/${saved.id}`);
    }
  }
  // 单人删除同样进回收站：5 秒内可就地撤销，之后回回收站找回
  async function remove(p: Person) {
    if (
      !(await ask({
        title: `删除联系人「${p.name}」及其所有关联记录？`,
        detail: '往来、对话、记账与纪念日会一并进回收站，' + RECOVER_NOTE,
        danger: true,
        confirmLabel: '删除'
      }))
    )
      return;
    if (await trashOne('person', p.id, load, `已删除「${p.name}」`)) {
      selectedIds = selectedIds.filter((id) => id !== p.id);
    }
  }
  async function unarchive(p: Person) {
    try {
      await API.delete(`/api/v1/people/${p.id}/archive`);
      toast.undoable('已取消归档', async () => {
        await API.post(`/api/v1/people/${p.id}/archive`, {}).catch(() => {});
        await load();
      });
      await load();
      selectedIds = selectedIds.filter((id) => id !== p.id);
    } catch (err) {
      toast.fail('取消归档失败', err);
    }
  }

  async function importVCard(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    importing = true;
    try {
      const text = await file.text();
      const res = await API.post<{
        total: number;
        imported: number;
        updated: number;
        skipped: number;
      }>('/api/v1/people/import/vcard', { text });
      const parts = [`成功导入 ${res.imported} 人`];
      if (res.updated > 0) parts.push(`更新姓名 ${res.updated} 人（与现有联系人按通讯录 ID 匹配）`);
      if (res.skipped > 0) parts.push(`跳过 ${res.skipped} 人（与现有联系人重复或缺少姓名）`);
      toast.ok(`vCard 导入完成：${parts.join('，')}`);
      await load();
    } catch (err) {
      toast.fail('导入失败', err);
    } finally {
      input.value = '';
      importing = false;
    }
  }

  async function exportVCard() {
    try {
      await API.download(
        '/api/v1/people/export/vcard',
        `qiansi-contacts-${new Date().toISOString().slice(0, 10)}.vcf`
      );
    } catch (err) {
      toast.fail('导出失败', err);
    }
  }
</script>

<div class="q-page">
  <PageHeader
    title="人物"
    subtitle={onlyArchived ? '归档只是隐藏，取消归档即可找回' : '管理你的联系人档案'}
  >
    {#snippet actions()}
      <input
        type="file"
        accept=".vcf,.vcard,text/vcard,text/x-vcard"
        class="hidden"
        bind:this={vcardInput}
        onchange={importVCard}
      />
      <button class="q-btn q-btn--ghost q-btn--sm" disabled={importing} onclick={exportVCard}>
        <Download size={14} /> 导出
      </button>
      <button
        class="q-btn q-btn--ghost q-btn--sm"
        disabled={importing}
        onclick={() => vcardInput?.click()}
      >
        <Upload size={14} />
        {importing ? '导入中…' : '导入'}
      </button>
      <button
        class="q-btn q-btn--sm"
        class:q-btn--primary={!selectMode}
        class:q-btn--danger-ghost={selectMode}
        onclick={toggleSelectMode}
      >
        {selectMode ? '退出选择' : '批量'}
      </button>
      <button class="q-btn q-btn--primary q-btn--sm" onclick={openCreate}>
        <Plus size={14} /> 新建
      </button>
    {/snippet}
  </PageHeader>

  <div class="q-stack">
    <!-- 筛选工具条 -->
    <div class="q-card q-card--tight flex flex-wrap items-center gap-2">
      <div class="q-seg">
        <button
          aria-pressed={!onlyArchived}
          class="q-seg__btn"
          onclick={() => {
            if (onlyArchived) toggleArchived();
          }}>联系人</button
        >
        <button
          aria-pressed={onlyArchived}
          class="q-seg__btn"
          onclick={() => {
            if (!onlyArchived) toggleArchived();
          }}>已归档</button
        >
      </div>
      <div class="relative min-w-[180px] flex-1">
        <Search
          size={14}
          class="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2"
          style="color: var(--q-faint);"
        />
        <input
          class="q-input !py-2 pl-9"
          placeholder="搜索姓名、备注…"
          bind:value={q}
          onkeydown={(e) => e.key === 'Enter' && load()}
        />
      </div>
      <select bind:value={selectedCat} onchange={() => load()} class="q-select !py-2">
        <option value={0}>全部圈子</option>
        {#each categories as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
      </select>
      <select bind:value={selectedTag} onchange={() => load()} class="q-select !py-2">
        <option value={0}>全部标签</option>
        {#each tags as t (t.id)}<option value={t.id}>{t.name}</option>{/each}
      </select>
      <select bind:value={selectedGender} onchange={() => load()} class="q-select !py-2">
        <option value="">全部性别</option>
        {#each GENDER_OPTIONS as o, i (i)}<option value={o.code}>{o.label}</option>{/each}
        <option value="none">未填</option>
      </select>
    </div>

    {#if selectMode}
      <div class="q-card q-card--tight flex flex-wrap items-center gap-2">
        <label class="flex cursor-pointer items-center gap-1.5 text-sm select-none">
          <input type="checkbox" checked={allVisibleSelected()} onchange={toggleAll} />
          全选本页
        </label>
        <span class="q-badge q-badge--theme">已选 {selectedIds.length} 位</span>
        <div class="flex-1"></div>
        <span class="text-xs" style="color: var(--q-muted);">批量更新</span>
        <select
          bind:value={bulkField}
          onchange={resetBulkValue}
          class="q-select q-btn--sm !py-1.5"
          disabled={selectedIds.length === 0}
        >
          {#each BULK_FIELDS as f, i (i)}<option value={f.key}>{f.label}</option>{/each}
        </select>
        {#if bulkDef.options}
          <select
            bind:value={bulkValue}
            class="q-select q-btn--sm !py-1.5"
            disabled={selectedIds.length === 0}
          >
            {#each bulkDef.options as o, i (i)}<option value={o.v}>{o.t}</option>{/each}
          </select>
        {:else}
          <input
            bind:value={bulkValue}
            placeholder="新的{bulkDef.label}"
            class="q-input q-btn--sm w-32 !py-1.5"
            disabled={selectedIds.length === 0}
          />
        {/if}
        <label
          class="flex cursor-pointer items-center gap-1.5 text-sm whitespace-nowrap select-none"
          title="勾上就只补没填过的人，已填的原值不动"
        >
          <input type="checkbox" bind:checked={bulkOnlyEmpty} />
          只填空着的
        </label>
        <button
          class="q-btn q-btn--primary q-btn--sm"
          disabled={selectedIds.length === 0 || busyBulk}
          onclick={applyBulkField}
        >
          {busyBulk ? '应用中…' : '应用'}
        </button>
        <select
          bind:value={bulkCat}
          class="q-select q-btn--sm !py-1.5"
          disabled={selectedIds.length === 0}
        >
          <option value={0}>加入圈子…</option>
          {#each categories as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
        </select>
        <button
          class="q-btn q-btn--primary q-btn--sm"
          disabled={selectedIds.length === 0 || bulkCat === 0}
          onclick={addSelectedToCircle}
        >
          加入
        </button>
        <button
          class="q-btn q-btn--danger-ghost q-btn--sm"
          disabled={selectedIds.length === 0}
          onclick={askDeleteSelected}
        >
          删除选中
        </button>
        <button class="q-btn q-btn--danger q-btn--sm" onclick={askClearAll}>清空全部</button>
      </div>
    {/if}

    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {#each list as p (p.id)}
        <div
          class="q-card q-card--tight flex items-start gap-3 transition-transform hover:-translate-y-0.5"
        >
          {#if selectMode}
            <input
              type="checkbox"
              class="mt-3 shrink-0"
              checked={selectedIds.includes(p.id)}
              onchange={() => toggleSelect(p.id)}
            />
          {/if}
          <a
            href={`/people/${p.id}`}
            class="flex min-w-0 flex-1 items-start gap-3"
            onclick={(e) => {
              e.preventDefault();
              if (selectMode) {
                toggleSelect(p.id);
              } else {
                navigate(`/people/${p.id}`);
              }
            }}
          >
            <div class="q-avatar h-11 w-11 text-base" style={`background: ${avatarBg(p.gender)};`}>
              {p.name.slice(0, 1)}
            </div>
            <div class="min-w-0 flex-1">
              <div class="truncate font-medium">{p.name}</div>
              {#if p.grade > 0 || (p.categories?.length ?? 0) > 0 || genderLabel(p.gender)}
                <div
                  class="mt-1 flex flex-wrap items-center gap-1 text-xs"
                  style="color: var(--q-muted);"
                >
                  {#if genderLabel(p.gender)}<span class="shrink-0">{genderLabel(p.gender)}</span
                    >{/if}
                  {#if p.grade > 0}
                    <span class="shrink-0" style="color: var(--q-warn);">{'♥'.repeat(p.grade)}</span
                    >
                  {/if}
                  {#each p.categories ?? [] as c (c.id)}
                    <span class="q-tag" style="--c: {c.color};">{c.name}</span>
                  {/each}
                </div>
              {/if}
              {#if p.stats}
                <div
                  class="q-num mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs"
                  style="color: var(--q-muted);"
                >
                  {#if p.stats.gift_out_fen || p.stats.gift_in_fen}
                    <span
                      >随出 ¥{yuanShort(p.stats.gift_out_fen)} / 收 ¥{yuanShort(
                        p.stats.gift_in_fen
                      )}</span
                    >
                    <span style={p.stats.net_fen !== 0 ? 'color: var(--q-text);' : ''}
                      >{netLabel(p.stats.net_fen)}</span
                    >
                  {/if}
                  {#if p.stats.last_contact}<span>最近 {contactAgo(p.stats.last_contact)}</span
                    >{/if}
                </div>
              {/if}
              {#if p.notes}
                <div class="mt-1 line-clamp-2 text-xs" style="color: var(--q-muted);">
                  {p.notes}
                </div>
              {/if}
            </div>
          </a>
          <div class="flex shrink-0 flex-col gap-0.5">
            {#if onlyArchived}
              <button class="q-iconbtn !h-7 !w-7" onclick={() => unarchive(p)} title="取消归档">
                <Undo2 size={14} />
              </button>
            {/if}
            <button class="q-iconbtn !h-7 !w-7" onclick={() => openEdit(p)} title="编辑">
              <Pencil size={13} />
            </button>
            <button class="q-iconbtn !h-7 !w-7" onclick={() => remove(p)} title="删除">
              <Trash2 size={14} />
            </button>
          </div>
        </div>
      {:else}
        <div class="col-span-full">
          {#if onlyArchived}
            <Empty icon={Users} text="没有已归档的联系人" />
          {:else}
            <Empty icon={Users} text="暂无联系人，新建一位，或导入 vCard 通讯录文件">
              {#snippet actions()}
                <button class="q-btn q-btn--primary q-btn--sm" onclick={openCreate}>
                  <Plus size={14} /> 新建联系人
                </button>
                <button class="q-btn q-btn--ghost q-btn--sm" onclick={() => vcardInput?.click()}>
                  <Upload size={14} /> 导入 vCard
                </button>
              {/snippet}
            </Empty>
          {/if}
        </div>
      {/each}
    </div>

    {#if hasMore}
      <div class="text-center">
        <button class="q-btn q-btn--ghost" onclick={loadMore}>
          加载更多（已显示 {list.length} 位）
        </button>
      </div>
    {/if}
  </div>
</div>

{#if showForm}
  <Modal wide onclose={() => (showForm = false)} title={editing ? '编辑联系人' : '新建联系人'}>
    <PersonForm
      person={editing}
      {categories}
      {tags}
      onsave={onSaved}
      onrelchange={() => load()}
      oncancel={() => (showForm = false)}
    />
  </Modal>
{/if}
