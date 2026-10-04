<script lang="ts">
  import { untrack, onMount } from 'svelte'
  import { API, type Person, type Category, type Tag } from './api'
  import { selfFirst, personLabel } from './self.svelte'
  import { dict, ensure } from './dict.svelte'
  import PersonRelEditor from './PersonRelEditor.svelte'

  let {
    person = null,
    categories = [],
    tags = [],
    onsave,
    oncancel,
    onrelchange,
  }: {
    person?: Person | null
    categories?: Category[]
    tags?: Tag[]
    onsave?: (saved: Person) => void
    oncancel?: () => void
    /** 关系变动时只刷新外层数据，不走 onsave，免得弹窗被关掉 */
    onrelchange?: () => void
  } = $props()

  type Form = {
    family_name: string; given_name: string; nickname: string; gender: string; grade: number; birthday: string
    birthday_is_lunar: boolean; phone: string; wechat: string; location: string
    notes: string
    /** 通过谁认识；空串 = 与我直接认识 */
    introduced_by_person_id: string
  }

  // 复姓表：编辑只有单一显示名的旧数据时用于正确拆分
  const COMPOUND_SURNAMES = ['欧阳','太史','端木','上官','司马','东方','独孤','南宫','万俟','闻人','夏侯','诸葛','尉迟','公羊','赫连','澹台','皇甫','濮阳','公冶','太叔','申屠','公孙','慕容','仲孙','钟离','长孙','宇文','司徒','鲜于','司空','闾丘','亓官','司寇','巫马','公西','颛孙','公良','漆雕','乐正','宰父','谷梁','拓跋','夹谷','轩辕','令狐','百里','呼延','东郭','南门','羊舌','微生','左丘','东门','西门','南荣','第五']

  const isHan = (s: string) => /\p{Script=Han}/u.test(s)

  // 旧数据只有单一显示名，编辑时尽力拆回姓/名；拆不准时整体归入名，用户可改
  function splitName(name: string): { family_name: string; given_name: string } {
    const n = name.trim()
    if (!n) return { family_name: '', given_name: '' }
    const chars = [...n]
    if (chars.every(isHan)) {
      for (const s of COMPOUND_SURNAMES) {
        if (n.startsWith(s)) return { family_name: s, given_name: n.slice(s.length) }
      }
      if (chars.length >= 2 && chars.length <= 3) {
        return { family_name: chars[0], given_name: n.slice(1) }
      }
      return { family_name: '', given_name: n }
    }
    if (n.includes(' ')) {
      const i = n.lastIndexOf(' ')
      return { family_name: n.slice(i + 1), given_name: n.slice(0, i) }
    }
    return { family_name: '', given_name: n }
  }

  // 显示名：中文按「姓+名」，西文按「名 姓」，与导入规则一致
  function composedName(): string {
    const f = form.family_name.trim(), g = form.given_name.trim()
    if (f && g) return isHan(f + g) ? f + g : `${g} ${f}`
    return f || g
  }

  let form = $state<Form>(emptyForm())
  let selectedTags = $state<number[]>([])
  let selectedCats = $state<number[]>([])
  let saving = $state(false)
  // 同名 / 同手机号 / 同微信的疑似重复记录
  let dups = $state<Person[]>([])
  let dupTimer: ReturnType<typeof setTimeout> | undefined

  async function checkDuplicates() {
    clearTimeout(dupTimer)
    const name = composedName().trim()
    const phone = (form.phone || '').trim()
    const wechat = (form.wechat || '').trim()
    if (!name && !phone && !wechat) { dups = []; return }
    dupTimer = setTimeout(async () => {
      try {
        const qs = new URLSearchParams({ name, phone, wechat })
        if (person?.id) qs.set('exclude_id', person.id)
        dups = await API.get(`/api/v1/people/duplicates?${qs}`) as Person[]
      } catch { dups = [] }
    }, 300)
  }

  function emptyForm(): Form {
    return { family_name: '', given_name: '', nickname: '', gender: '', grade: 0, birthday: '', birthday_is_lunar: false, phone: '', wechat: '', location: '', notes: '', introduced_by_person_id: '' }
  }

  // 引荐人候选：新建时也要能选，所以弹窗一挂载就要名单（走共享字典缓存）
  const peopleOptions = $derived(dict.people)
  onMount(() => { ensure('people') })
  // 不能把自己设为自己的引荐人；其余人按「本人优先」排序
  const introducerOptions = $derived(selfFirst(peopleOptions, person?.id ?? ''))

  // 人物对象变化（切换编辑目标）时重新灌入表单与标签
  $effect(() => {
    const p = person
    if (p) {
      const filled: Form = {
        family_name: p.family_name || '', given_name: p.given_name || '',
        nickname: p.nickname || '', gender: p.gender || '', grade: p.grade,
        birthday: p.birthday || '', birthday_is_lunar: !!p.birthday_is_lunar,
        phone: p.phone || '', wechat: p.wechat || '', location: p.location || '',
        notes: p.notes || '', introduced_by_person_id: p.introduced_by_person_id || '',
      }
      // untrack：这个 effect 里要回写 selectedCats，把 p.categories 也记成依赖会自触发，
      // 弹窗一打开就无限重跑（表现为 taggings/of 请求风暴）。
      selectedCats = untrack(() => (p.categories || []).map(c => c.id))
      // 旧数据没有姓/名结构：按启发式拆分回填，拆错可手动改。
      // 直接合并进新对象：{ ...form, ... } 会把 form 读成依赖又回写 form，
      // 对没有姓/名拆分的人无限自触发（effect_update_depth_exceeded + 请求风暴）。
      if (!p.family_name && !p.given_name && p.name) {
        Object.assign(filled, splitName(p.name))
      }
      form = filled
      loadTags(p.id)
    } else {
      form = emptyForm()
      selectedTags = []
      selectedCats = []
    }
  })

  async function loadTags(id: string) {
    try {
      const owned = await API.get(`/api/v1/taggings/of?target_type=person&target_id=${id}`) as Tag[]
      selectedTags = owned.map(t => t.id)
    } catch {
      selectedTags = []
    }
  }

  function toggleTag(id: number) {
    selectedTags = selectedTags.includes(id) ? selectedTags.filter(x => x !== id) : [...selectedTags, id]
  }

  function toggleCat(id: number) {
    selectedCats = selectedCats.includes(id) ? selectedCats.filter(x => x !== id) : [...selectedCats, id]
  }

  // 合并：把疑似重复的那条并进当前正在编辑的人物
  async function mergeInto(targetId: string, fromId: string, fromName: string) {
    if (!confirm(`把「${fromName}」的所有记录合并到当前人物，并删除「${fromName}」？此操作不可撤销。`)) return
    try {
      await API.post(`/api/v1/people/${targetId}/merge`, { from: fromId })
      dups = dups.filter(d => d.id !== fromId)
      alert('已合并')
      onsave?.({ ...(person as Person) })
    } catch (err: any) {
      alert('合并失败：' + (err?.message || err))
    }
  }

  async function submit() {
    if (!composedName().trim()) { alert('姓、名至少填一项'); return }
    saving = true
    try {
      const body: any = { ...form, name: composedName().trim() }
      // 圈子可多选，随整行 PUT 一起提交；空数组即「不归入任何圈子」
      body.category_ids = selectedCats
      let saved: Person
      if (person?.id) {
        saved = await API.put(`/api/v1/people/${person.id}`, body) as Person
      } else {
        saved = await API.post('/api/v1/people', body) as Person
      }
      // 标签：先取当前集合，做差集增删
      const before = (await API.get(`/api/v1/taggings/of?target_type=person&target_id=${saved.id}`) as Tag[]).map(t => t.id)
      for (const id of selectedTags.filter(x => !before.includes(x))) {
        await API.post('/api/v1/taggings/add', { target_type: 'person', target_id: saved.id, tag_id: id })
      }
      for (const id of before.filter(x => !selectedTags.includes(x))) {
        await API.post('/api/v1/taggings/remove', { target_type: 'person', target_id: saved.id, tag_id: id })
      }
      onsave?.(saved)
    } catch (err: any) {
      alert('保存失败：' + (err?.message || err))
    } finally {
      saving = false
    }
  }
</script>

<div class="space-y-3">
  <div class="grid grid-cols-2 gap-3">
    <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" placeholder="姓"
           style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);"
           bind:value={form.family_name} oninput={checkDuplicates} />
    <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" placeholder="名 *"
           style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);"
           bind:value={form.given_name} oninput={checkDuplicates} />
  </div>
  {#if dups.length > 0}
    <div class="rounded-lg p-2 text-xs" style="background: color-mix(in srgb, #f59e0b 12%, transparent); color: #b45309;">
      <div class="font-medium mb-1">发现 {dups.length} 位可能重复的联系人</div>
      <ul class="space-y-1">
        {#each dups as d}
          <li class="flex items-center gap-2">
            <span class="truncate">{d.name}{d.phone ? ` · ${d.phone}` : ''}{d.wechat ? ` · 微信 ${d.wechat}` : ''}</span>
            {#if person?.id}
              <button class="ml-auto shrink-0 underline" onclick={() => mergeInto(person!.id, d.id, d.name)}>合并过来</button>
            {/if}
          </li>
        {/each}
      </ul>
    </div>
  {/if}
  <div class="grid grid-cols-2 gap-3">
    <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" placeholder="昵称"
           style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" bind:value={form.nickname} />
    <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" placeholder="性别"
           style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" bind:value={form.gender} />
  </div>
  <div class="grid grid-cols-2 gap-3 items-center">
    <input type="date" class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" bind:value={form.birthday} />
    <label class="text-sm flex items-center gap-2" style="color: var(--q-muted);">
      <input type="checkbox" bind:checked={form.birthday_is_lunar} /> 农历生日
    </label>
  </div>
  {#if form.birthday}
    <p class="text-xs" style="color: var(--q-muted);">保存后会自动生成一条「{composedName() || 'TA'}的生日」纪念日并进入提醒。</p>
  {/if}
  <div class="grid grid-cols-2 gap-3">
    <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" placeholder="电话"
           style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" bind:value={form.phone} oninput={checkDuplicates} />
    <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" placeholder="微信"
           style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" bind:value={form.wechat} oninput={checkDuplicates} />
  </div>
  <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" placeholder="位置"
         style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" bind:value={form.location} />
  <div>
    <div class="text-xs mb-1" style="color: var(--q-muted);">亲密度 1–5{form.grade === 0 ? '（当前未设置）' : ''}</div>
    <div class="flex gap-1">
      {#each [1,2,3,4,5] as g}
        <button class="w-8 h-8 rounded-md text-sm font-semibold transition"
                style={form.grade >= g ? 'background: var(--q-theme); color: white;' : 'background: var(--q-bg); color: var(--q-muted); border: 1px solid var(--q-border);'}
                onclick={() => form.grade = g}>{g}</button>
      {/each}
      <button class="h-8 px-2 rounded-md text-sm transition" title="清除亲密度"
              style="background: var(--q-bg); color: var(--q-muted); border: 1px solid var(--q-border);"
              onclick={() => form.grade = 0}>清除</button>
    </div>
  </div>
  <div>
    <div class="text-xs mb-1" style="color: var(--q-muted);">圈子（可多选）</div>
    {#if categories.length > 0}
      <div class="flex flex-wrap gap-1">
        {#each categories as c}
          <button class="text-xs px-2 py-1 rounded-full transition"
                  style={selectedCats.includes(c.id)
                    ? `background: color-mix(in srgb, ${c.color} 22%, transparent); color: ${c.color}; border: 1px solid ${c.color};`
                    : 'background: var(--q-bg); color: var(--q-muted); border: 1px solid var(--q-border);'}
                  onclick={() => toggleCat(c.id)}>{c.name}</button>
        {/each}
      </div>
    {:else}
      <p class="text-xs" style="color: var(--q-muted);">还没有圈子，去「设置 › 圈子（分组）」建一个。</p>
    {/if}
  </div>
  {#if tags.length > 0}
    <div>
      <div class="text-xs mb-1" style="color: var(--q-muted);">标签</div>
      <div class="flex flex-wrap gap-1">
        {#each tags as t}
          <button class="text-xs px-2 py-1 rounded-full transition"
                  style={selectedTags.includes(t.id)
                    ? `background: color-mix(in srgb, ${t.color} 22%, transparent); color: ${t.color}; border: 1px solid ${t.color};`
                    : 'background: var(--q-bg); color: var(--q-muted); border: 1px solid var(--q-border);'}
                  onclick={() => toggleTag(t.id)}>{t.name}</button>
        {/each}
      </div>
    </div>
  {/if}
  <div>
    <div class="text-xs mb-1" style="color: var(--q-muted);">通过谁认识（引荐人，留空 = 直接认识）</div>
    <select bind:value={form.introduced_by_person_id}
            class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
      <option value="">直接认识</option>
      {#each introducerOptions as p}<option value={p.id}>{personLabel(p)}</option>{/each}
    </select>
    <p class="text-xs mt-1" style="color: var(--q-muted);">多层关系（同学的对象的闺蜜…）可在关系图上用「引荐链」一次录入。</p>
  </div>
  <textarea class="w-full px-3 py-2 rounded-lg text-sm outline-none min-h-[80px]" placeholder="备注、爱好、口味…"
            style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" bind:value={form.notes}></textarea>

  <!-- 关系挂在独立的 relationships 表上，只有已存在的联系人才能配关系 -->
  {#if person?.id}
    <div class="pt-3 mt-1" style="border-top: 1px solid var(--q-border);">
      <h3 class="text-sm font-medium mb-2">关系</h3>
      <PersonRelEditor personId={person.id} onchange={() => onrelchange?.()} />
    </div>
  {/if}
</div>

<div class="flex justify-end gap-2 mt-5">
  <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" onclick={() => oncancel?.()}>取消</button>
  <button class="px-4 py-2 rounded-lg text-sm text-white disabled:opacity-60" style="background: var(--q-theme);" disabled={saving} onclick={submit}>
    {saving ? '保存中…' : '保存'}
  </button>
</div>
