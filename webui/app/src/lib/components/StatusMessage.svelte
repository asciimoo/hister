<script lang="ts">
  type StatusType = 'error' | 'success' | 'loading' | 'empty';

  let {
    message,
    type = 'success',
    show = true,
    className = '',
  }: {
    message: string;
    type?: StatusType;
    show?: boolean;
    className?: string;
  } = $props();

  const typeStyles: Record<StatusType, string> = {
    error: 'border-hister-rose bg-hister-rose/10 text-hister-rose',
    success: 'border-hister-teal bg-hister-teal/10 text-hister-teal',
    loading: 'border-border-brand-muted bg-muted-surface text-text-brand-muted',
    empty: 'text-text-brand-muted',
  };

  const wrapperStyles: Record<StatusType, string> = {
    error: 'px-4 py-3 border-[3px]',
    success: 'px-4 py-3 border-[3px]',
    loading: 'flex items-center justify-center py-16',
    empty: 'flex items-center justify-center py-16 text-lg',
  };
</script>

{#if show && message}
  <div class="{wrapperStyles[type]} {typeStyles[type]} {className}">
    {#if type === 'loading'}
      <span class="font-inter">{message}</span>
    {:else if type === 'empty'}
      <span class="font-inter text-lg">{message}</span>
    {:else}
      <span class="font-inter text-sm">{message}</span>
    {/if}
  </div>
{/if}
