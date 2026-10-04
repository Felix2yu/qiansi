<script lang="ts">
  import { askBox, answerAsk } from './ask.svelte'

  // 确认框宿主：挂在 App 上，全站共用一个。
  let box: HTMLButtonElement | undefined = $state(undefined)
  // 弹窗一出现就把焦点放到「取消」：回车不该顺手确认删除
  const a = $derived(askBox.pending)
  $effect(() => { if (a) requestAnimationFrame(() => box?.focus()) })
</script>

{#if a}
  <div class="fixed inset-0 z-[70] flex items-center justify-center p-4" role="alertdialog" aria-modal="true" aria-label={a.title}>
    <button type="button" aria-label="关闭确认框" class="absolute inset-0 cursor-default"
            style="background: rgba(0,0,0,.3);" onclick={() => answerAsk(false)}></button>
    <div class="relative w-full max-w-md rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <h2 class="font-semibold" style={a.danger ? 'color: var(--q-danger);' : ''}>{a.title}</h2>
      {#if a.detail}
        <p class="text-sm leading-relaxed mt-2" style="color: var(--q-muted);">{a.detail}</p>
      {/if}
      <div class="flex justify-end gap-2 mt-5">
        <button bind:this={box} class="px-4 py-2 rounded-lg text-sm"
                style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);"
                onclick={() => answerAsk(false)}>{a.cancelLabel || '取消'}</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white"
                style="background: {a.danger ? 'var(--q-danger)' : 'var(--q-theme)'};"
                onclick={() => answerAsk(true)}>{a.confirmLabel || '确定'}</button>
      </div>
    </div>
  </div>
{/if}
