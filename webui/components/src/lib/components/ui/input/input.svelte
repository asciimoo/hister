<script lang="ts">
  import type { HTMLInputAttributes, HTMLInputTypeAttribute } from 'svelte/elements';
  import { cn, type WithElementRef } from '@nogon/components/utils';
  import { tv } from 'tailwind-variants';

  const inputVariants = tv({
    base: 'flex w-full min-w-0 transition-[color,box-shadow] outline-none disabled:cursor-not-allowed disabled:opacity-50',
    variants: {
      variant: {
        default: [
          'border-input bg-background selection:bg-primary selection:text-primary-foreground ring-offset-background placeholder:text-muted-foreground h-9 border px-3 py-1 text-base md:text-sm',
          'focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]',
          'aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive',
        ],
        brutal: [
          'h-12 px-4 bg-page-bg border-[3px] border-brutal-border font-fira text-sm text-text-brand placeholder:text-text-brand-muted shadow-none',
          'focus-visible:ring-0 focus-visible:border-nogon-indigo transition-colors',
        ],
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  });

  type InputVariant = 'default' | 'brutal';
  type InputType = Exclude<HTMLInputTypeAttribute, 'file'>;

  type Props = WithElementRef<
    Omit<HTMLInputAttributes, 'type'> &
      ({ type: 'file'; files?: FileList } | { type?: InputType; files?: undefined })
  > & { variant?: InputVariant };

  let {
    ref = $bindable(null),
    value = $bindable(),
    type,
    files = $bindable(),
    variant = 'default',
    class: className,
    'data-slot': dataSlot = 'input',
    ...restProps
  }: Props = $props();
</script>

{#if type === 'file'}
  <input
    bind:this={ref}
    data-slot={dataSlot}
    class={cn(
      'selection:bg-primary selection:text-primary-foreground border-input ring-offset-background placeholder:text-muted-foreground flex h-9 w-full min-w-0 border bg-transparent px-3 pt-1.5 text-sm font-medium transition-[color,box-shadow] outline-none disabled:cursor-not-allowed disabled:opacity-50',
      'focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]',
      'aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive',
      className,
    )}
    type="file"
    bind:files
    bind:value
    {...restProps}
  />
{:else}
  <input
    bind:this={ref}
    data-slot={dataSlot}
    class={cn(inputVariants({ variant }), className)}
    {type}
    bind:value
    {...restProps}
  />
{/if}
