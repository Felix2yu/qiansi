<script lang="ts">
  import { X } from '@lucide/svelte'

  // 危险操作确认框：必须手打确认词，按钮才亮。
  // confirm() 的默认焦点就在「确定」上，肌肉记忆按回车就是一份不可恢复的删除；
  // 要输入这个词，手指就得先动一次，而这正是「误点即整本账消失」需要拦的那一下。
  let {
    word,
    title,
    detail = '',
    confirmLabel = '确认删除',
    onconfirm,
    oncancel,
  }: {
    word: string
    title: string
    detail?: string
    confirmLabel?: string
    onconfirm: () => void | Promise<void>
    oncancel: () => void
  } = $props()

  let input = $state('')
  let box: HTMLInputElement | undefined = $state()
  let busy = $state(false)
  const armed = $derived(input.trim() === word)

  $effect(() => { box?.focus() })

  function close() { oncancel() }
  // 只负责转发动作，关窗由调用方在跑完之后做：
  // 在这里先 oncancel() 会把弹窗连同它的 props 一起拆掉，再读 onconfirm 就晚了。
  async function submit() {
    if (!armed || busy) return
    busy = true
    try {
      await onconfirm()
    } finally {
      busy = false
    }
  }
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') close() }} />

<div class="fixed inset-0 z-50 flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-label={title}>
  <button type="button" aria-label="关闭确认框" class="absolute inset-0 cursor-default" style="background: rgba(0,0,0,0.3); border: 0;" onclick={close}></button>
  <div class="relative w-full max-w-md rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <div class="flex items-center justify-between mb-3">
      <h2 class="font-semibold" style="color: #dc2626;">{title}</h2>
      <button onclick={close}><X size={18} /></button>
    </div>
    {#if detail}
      <p class="text-sm leading-relaxed mb-3" style="color: var(--q-muted);">{detail}</p>
    {/if}
    <p class="text-sm mb-2">
      请输入 <span class="font-mono font-semibold" style="color: #dc2626;">{word}</span> 以确认
    </p>
    <input
      bind:this={box}
      bind:value={input}
      class="w-full px-3 py-2 rounded-lg text-sm outline-none"
      style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);"
      placeholder={word}
      autocomplete="off"
      onkeydown={(e) => { if (e.key === 'Enter') submit() }}
    />
    <div class="flex justify-end gap-2 mt-5">
      <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={close}>取消</button>
      <button
        class="px-4 py-2 rounded-lg text-sm text-white"
        style="background: {armed ? '#dc2626' : 'var(--q-border)'};"
        disabled={!armed || busy}
        onclick={submit}>{busy ? '处理中…' : confirmLabel}</button>
    </div>
  </div>
</div>
