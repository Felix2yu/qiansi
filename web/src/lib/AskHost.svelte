<script lang="ts">
  import { askBox, answerAsk } from './ask.svelte';

  // 确认框宿主：挂在 App 上，全站共用一个。
  let box: HTMLButtonElement | undefined = $state(undefined);
  // 弹窗一出现就把焦点放到「取消」：回车不该顺手确认删除
  const a = $derived(askBox.pending);
  $effect(() => {
    if (a) requestAnimationFrame(() => box?.focus());
  });
</script>

{#if a}
  <div class="q-modal z-[70]" role="alertdialog" aria-modal="true" aria-label={a.title}>
    <button
      type="button"
      aria-label="关闭确认框"
      class="q-modal__veil"
      onclick={() => answerAsk(false)}
    ></button>
    <div class="q-modal__panel" role="document">
      <div class="q-modal__body !pt-9">
        <h2 class="q-modal__title mb-2" style={a.danger ? 'color: var(--q-danger);' : ''}>
          {a.title}
        </h2>
        {#if a.detail}
          <p class="text-sm leading-relaxed" style="color: var(--q-muted);">{a.detail}</p>
        {/if}
        <div class="mt-5 flex justify-end gap-2">
          <button bind:this={box} class="q-btn q-btn--ghost" onclick={() => answerAsk(false)}
            >{a.cancelLabel || '取消'}</button
          >
          <button
            class="q-btn"
            class:q-btn--danger={a.danger}
            class:q-btn--primary={!a.danger}
            onclick={() => answerAsk(true)}>{a.confirmLabel || '确定'}</button
          >
        </div>
      </div>
    </div>
  </div>
{/if}
