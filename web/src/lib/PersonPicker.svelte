<script lang="ts">
  import { onDestroy } from 'svelte'
  import { selfFirst, personLabel, loadSelf } from './self.svelte'
  import { searchPeople, resolvePerson, cachedPerson, type PersonLite } from './personSearch'
  import { X, Search } from '@lucide/svelte'

  // 选人控件：输入即搜，不再依赖「先把 500 人全拉下来」的名单。
  // 展开时原地换成搜索框，免得在弹窗里被 overflow 裁掉。
  let {
    value = $bindable(''),
    selectedName = '',
    placeholder = '搜索联系人',
    clearable = true,
    exclude = [],
    compact = false,
    onchange,
  }: {
    value?: string
    /** 调用方已知姓名时直接传进来，省一次回查 */
    selectedName?: string
    placeholder?: string
    /** 必选的场景（关系两端、待办归属）不给清除按钮 */
    clearable?: boolean
    exclude?: string[]
    /** 行内/窄栏用的小号样式 */
    compact?: boolean
    onchange?: (picked: PersonLite | null) => void
  } = $props()

  let open = $state(false)
  // 显示名要能标出「我 · xxx」；settings 只拉一次，多个实例共用同一趟请求
  void loadSelf()
  let query = $state('')
  let results = $state<PersonLite[]>([])
  let selected = $state<PersonLite | null>(null)
  let searching = $state(false)
  let failed = $state(false)
  let active = $state(0)
  let timer: ReturnType<typeof setTimeout> | undefined
  let wrap: HTMLDivElement | undefined = $state()
  let input: HTMLInputElement | undefined = $state()

  const fieldCls = $derived(compact
    ? 'w-full px-2 py-1 rounded-md text-xs outline-none'
    : 'w-full px-3 py-2 rounded-lg text-sm outline-none')
  const fieldStyle = 'background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);'

  // 外部改了 value（回填表单、清空筛选）时跟着换显示名
  $effect(() => {
    const v = value
    if (!v) {
      if (selected) selected = null
      return
    }
    if (selected?.id === v) return
    if (selectedName) {
      selected = { id: v, name: selectedName }
      return
    }
    const hit = cachedPerson(v)
    if (hit) { selected = hit; return }
    void resolvePerson(v).then(p => { if (p && value === p.id) selected = p })
  })

  function schedule() {
    if (timer) clearTimeout(timer)
    timer = setTimeout(run, 200)
  }

  async function run() {
    searching = true
    failed = false
    try {
      // 本人排最前是这个控件的承诺，不能指望宿主页先拉过 settings
      await loadSelf()
      const list = await searchPeople(query.trim(), exclude)
      results = selfFirst(list)
      active = 0
    } catch {
      failed = true
      results = []
    } finally {
      searching = false
    }
  }

  async function show() {
    open = true
    // 空关键词也先给一屏常用联系人，点一下就能选，不必逼用户猜名字
    await run()
    input?.focus()
  }

  function hide() {
    open = false
    query = ''
  }

  function pick(p: PersonLite) {
    selected = p
    value = p.id
    onchange?.(p)
    hide()
  }

  function clear(e: MouseEvent) {
    e.stopPropagation()
    selected = null
    value = ''
    onchange?.(null)
  }

  function onKeydown(e: KeyboardEvent) {
    if (!open) return
    if (e.key === 'Escape') { hide(); return }
    if (e.key === 'ArrowDown') { active = Math.min(active + 1, results.length - 1); e.preventDefault(); return }
    if (e.key === 'ArrowUp') { active = Math.max(active - 1, 0); e.preventDefault(); return }
    if (e.key === 'Enter') {
      const p = results[active]
      if (p) { pick(p); e.preventDefault() }
      return
    }
  }

  function onDocClick(e: MouseEvent) {
    if (open && wrap && !wrap.contains(e.target as Node)) hide()
  }
  document.addEventListener('mousedown', onDocClick)
  onDestroy(() => {
    document.removeEventListener('mousedown', onDocClick)
    if (timer) clearTimeout(timer)
  })
</script>

<svelte:window onkeydown={onKeydown} />

<div class="relative" bind:this={wrap}>
  {#if !open}
    <div class="flex items-center gap-1">
      <button type="button" class={fieldCls} style={fieldStyle} onclick={show}
              aria-label={selected ? personLabel(selected) : placeholder}>
        <span style={selected ? '' : 'color: var(--q-muted);'}>
          {selected ? personLabel(selected) : placeholder}
        </span>
      </button>
      {#if clearable && value}
        <button type="button" class="shrink-0 p-1 rounded" style="color: var(--q-muted);" title="清除"
                onclick={clear}><X size={compact ? 12 : 14} /></button>
      {/if}
    </div>
  {:else}
    <div class="absolute left-0 right-0 top-0 z-30 rounded-lg" style="background: var(--q-surface); border: 1px solid var(--q-border); box-shadow: 0 6px 18px rgba(0,0,0,.15);">
      <div class="flex items-center gap-1 px-2 py-1">
        <Search size={compact ? 12 : 14} style="color: var(--q-muted); flex: none;" />
        <input bind:this={input} bind:value={query} oninput={schedule}
               class="flex-1 py-1.5 text-sm outline-none" style="background: transparent; color: var(--q-text); border: 0;"
               placeholder={placeholder} aria-label="搜索联系人" />
        <button type="button" class="p-1 rounded" style="color: var(--q-muted);" title="收起" onclick={hide}><X size={14} /></button>
      </div>
      <div class="max-h-56 overflow-y-auto" style="border-top: 1px solid var(--q-border);">
        {#if searching && results.length === 0}
          <p class="px-3 py-2 text-xs" style="color: var(--q-muted);">搜索中…</p>
        {:else if failed}
          <p class="px-3 py-2 text-xs" style="color: var(--q-danger);">搜索失败，请重试</p>
        {:else if results.length === 0}
          <p class="px-3 py-2 text-xs" style="color: var(--q-muted);">没有匹配的人 · 换个关键词，或先到通讯录里新建</p>
        {:else}
          {#each results as p, i}
            <button type="button" class="w-full text-left px-3 py-2 text-sm"
                    style={i === active ? 'background: var(--q-bg);' : 'color: var(--q-text);'}
                    onmouseenter={() => (active = i)} onclick={() => pick(p)}>
              {personLabel(p)}
              {#if p.nickname && p.nickname !== p.name}<span class="ml-1 text-xs" style="color: var(--q-muted);">{p.nickname}</span>{/if}
            </button>
          {/each}
        {/if}
      </div>
    </div>
  {/if}
</div>
