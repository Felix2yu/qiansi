<script lang="ts">
  import type { Snippet } from 'svelte';

  /** 全站统一页头：朱丝短杠 + 宋体大标题 + 副题 + 右侧操作区 */
  let {
    title,
    subtitle = '',
    actions
  }: {
    title: string;
    subtitle?: string;
    actions?: Snippet;
  } = $props();
</script>

<header class="q-head">
  <div class="q-head__main">
    <span class="q-head__bar" aria-hidden="true"></span>
    <div class="min-w-0">
      <h1 class="q-title">{title}</h1>
      {#if subtitle}
        <p class="q-subtitle">{subtitle}</p>
      {/if}
    </div>
  </div>
  {#if actions}
    <div class="q-head__actions">
      {@render actions()}
    </div>
  {/if}
</header>
