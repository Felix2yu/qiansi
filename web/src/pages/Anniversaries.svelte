<script lang="ts">
  import { onMount } from 'svelte'
  import { API, todayLocal, type Anniversary } from '../lib/api'
  import { loadSelf } from '../lib/self.svelte'
  import PersonPicker from '../lib/PersonPicker.svelte'
  import { toast } from '../lib/toast.svelte'
  import { ask } from '../lib/ask.svelte'
  import { Plus, X } from '@lucide/svelte'

  let list = $state<Anniversary[]>([])
  let showForm = $state(false)
  let editId = $state('')
  let form = $state(emptyForm())
  // 编辑时把纪念日自带的名字交给选人控件显示，省一次回查
  let pickedName = $state('')

  function emptyForm() {
    return { person_id: '', title: '', date: todayLocal(), is_lunar: false, repeat_yearly: true, remind_days: '7,3,1,0' }
  }

  async function load() {
    list = await API.get<Anniversary[]>('/api/v1/anniversaries').catch((err: any) => {
      // 读失败也不能装作「暂无」：空列表和查不到是两件事
      toast.fail('加载失败', err)
      return [] as Anniversary[]
    }) || []
  }
  onMount(() => {
    loadSelf()
    load()
  })

  function openCreate() {
    editId = ''
    form = emptyForm()
    pickedName = ''
    showForm = true
  }

  function openEdit(a: Anniversary) {
    editId = a.id
    pickedName = a.person_name || ''
    form = {
      person_id: a.person_id || '', title: a.title, date: (a.date || '').slice(0, 10),
      is_lunar: !!a.is_lunar, repeat_yearly: !!a.repeat_yearly, remind_days: a.remind_days || '7,3,1,0',
    }
    showForm = true
  }

  async function submit() {
    if (!form.title.trim() || !form.date) { toast.error('标题和日期必填'); return }
    const body: any = { ...form }
    if (!body.person_id) delete body.person_id
    if (!body.remind_days.trim()) body.remind_days = '0'
    const isEdit = !!editId
    try {
      if (isEdit) await API.put(`/api/v1/anniversaries/${editId}`, body)
      else await API.post('/api/v1/anniversaries', body)
    } catch (err: any) {
      // 失败时弹窗不关、内容不丢，改完可以直接再存一次
      toast.fail('保存失败', err)
      return
    }
    showForm = false
    editId = ''
    toast.ok(isEdit ? '已更新' : '已保存')
    await load()
  }

  // 硬删除没有反悔余地：服务端没有可恢复的中间态，这里靠确认框挡一下
  async function remove(a: Anniversary) {
    if (!(await ask({ title: `删除「${a.title}」？`, detail: '删除后无法恢复。', danger: true, confirmLabel: '删除' }))) return
    try {
      await API.delete(`/api/v1/anniversaries/${a.id}`)
      toast.ok('已删除')
      await load()
    } catch (err: any) {
      toast.fail('删除失败', err)
    }
  }

  /** 每年重复的纪念日只有月日有意义，展示时补上今年的年份便于阅读 */
  function displayDate(a: Anniversary) {
    const d = new Date(a.date)
    return a.repeat_yearly ? `${d.getMonth() + 1}月${d.getDate()}日` : d.toLocaleDateString()
  }

  /** 角标显示下一次发生的月日（循环/农历项取后端换算后的公历日期） */
  function badgeDate(a: Anniversary) {
    const d = new Date(a.next_date || a.date)
    return { m: d.getMonth() + 1, d: d.getDate() }
  }

  /** 倒计时文案；null 表示不展示 chip */
  function countdownLabel(a: Anniversary): string | null {
    if (a.days_until == null) return a.repeat_yearly ? null : '已过'
    if (a.days_until === 0) return '今天'
    return `${a.days_until} 天`
  }
</script>
<div class="space-y-4">
  <header class="flex items-center justify-between">
    <div><h1 class="text-2xl font-semibold">纪念日</h1><p class="text-sm mt-1" style="color: var(--q-muted);">生日、周年、倒数日</p></div>
    <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={openCreate}>
      <Plus size={14} /> 新建
    </button>
  </header>
  <ul class="space-y-2">
    {#each list as a}
      {@const bd = badgeDate(a)}
      {@const cd = countdownLabel(a)}
      <li class="rounded-lg p-3 flex items-center gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="w-10 h-10 rounded-lg flex flex-col items-center justify-center text-white shrink-0" style="background: var(--q-theme);">
          <div class="text-[10px] leading-none">{bd.m}月</div>
          <div class="text-base font-semibold leading-none mt-0.5">{bd.d}</div>
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-sm font-medium truncate">{a.title}</div>
          <div class="text-xs mt-0.5" style="color: var(--q-muted);">
            {a.person_name && `${a.person_name} · `}
            {a.is_lunar ? '农历 ' : ''}{displayDate(a)}
            {a.repeat_yearly ? ' · 每年循环' : ''}
          </div>
        </div>
        {#if cd}
          <span class="text-xs px-2 py-0.5 rounded-full shrink-0"
            style={a.days_until != null && a.days_until <= 7
              ? 'background: var(--q-theme); color: #fff;'
              : 'background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-muted);'}>{cd}</span>
        {/if}
        <button class="text-xs px-2 py-1 rounded" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => openEdit(a)}>编辑</button>
        <button class="text-xs" style="color: var(--q-muted);" onclick={() => remove(a)}>删</button>
      </li>
    {:else}<li class="text-center py-8 text-sm" style="color: var(--q-muted);">暂无</li>{/each}
  </ul>
</div>

{#if showForm}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4">
    <button type="button" aria-label="关闭弹窗" class="absolute inset-0 cursor-default" style="background: rgba(0,0,0,0.3); border: 0;" onclick={() => { showForm = false; editId = '' }}></button>
    <div class="relative w-full max-w-lg rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4"><h2 class="font-semibold">{editId ? '编辑纪念日' : '新建纪念日'}</h2><button onclick={() => { showForm = false; editId = '' }}><X size={18} /></button></div>
      <div class="space-y-3">
        <PersonPicker bind:value={form.person_id} selectedName={pickedName} placeholder="自属（倒数日）" />
        <input bind:value={form.title} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="标题（如：妈妈生日）" />
        <input type="date" bind:value={form.date} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
        <div class="flex items-center gap-4">
          <label class="text-sm flex items-center gap-2" style="color: var(--q-muted);"><input type="checkbox" bind:checked={form.is_lunar} /> 农历</label>
          <label class="text-sm flex items-center gap-2" style="color: var(--q-muted);"><input type="checkbox" bind:checked={form.repeat_yearly} /> 每年循环</label>
        </div>
        <input bind:value={form.remind_days} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="提醒天数（逗号分隔，如 7,3,1,0）" />
      </div>
      <div class="flex justify-end gap-2 mt-5">
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => { showForm = false; editId = '' }}>取消</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submit}>保存</button>
      </div>
    </div>
  </div>
{/if}
