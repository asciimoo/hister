<script lang="ts">
  import { Badge } from '@hister/components/ui/badge';
  import { Button } from '@hister/components/ui/button';
  import { Input } from '@hister/components/ui/input';
  import { Switch } from '@hister/components/ui/switch';
  import * as Tooltip from '@hister/components/ui/tooltip';
  import { Eye, Info, Lock, Pencil } from '@lucide/svelte';
  import {
    canEditSetting,
    sourceLabel,
    type SettingField,
    type SettingValue,
    type SettingEdit,
  } from '#lib/settings.js';

  let {
    field,
    active,
    edit,
    errors = [],
    disabled = false,
    onEdit,
  }: {
    field: SettingField;
    active: SettingValue;
    edit?: SettingEdit;
    errors?: string[];
    disabled?: boolean;
    onEdit: (edit: SettingEdit | undefined) => void;
  } = $props();
  let sourceOpen = $state(false);
  const editable = $derived(canEditSetting(field, active));
  const value = $derived(
    edit?.action === 'set' ? edit.value : edit?.action === 'reset' ? field.default : active.value,
  );
  const id = $derived(`setting-${field.path}`);
  const description = $derived(
    `${id}-description${edit ? ` ${id}-status` : ''}${errors.length ? ` ${id}-errors` : ''}`,
  );
  const controlClass =
    'border-input bg-background text-text-brand focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 h-10 w-full min-w-0 border px-3 py-2 text-base transition-[color,box-shadow] outline-none focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50 md:text-sm';
  function set(value: unknown) {
    onEdit(value === active.value ? undefined : { path: field.path, action: 'set', value });
  }
  function display(value: unknown): string {
    if (value === undefined || value === null) return 'Not available';
    if (value === '') return 'Empty';
    if (typeof value === 'boolean') return value ? 'Enabled' : 'Disabled';
    return String(value);
  }
</script>

<section
  aria-label={field.label}
  class="border-border-brand -mx-3 mb-2 grid gap-x-8 gap-y-3 border-b border-l-2 px-3 py-4 last:mb-0 last:border-b-0 lg:grid-cols-[minmax(0,1fr)_minmax(0,24rem)] {active
    .source.locked
    ? 'border-l-hister-amber/60 bg-hister-amber/10'
    : !editable
      ? 'border-l-border-brand bg-muted-surface/50'
      : edit
        ? 'bg-hister-indigo/5 border-l-transparent'
        : 'border-l-transparent'}"
>
  <div class="min-w-0 space-y-1 break-words">
    <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
      <div class="flex min-w-0 flex-wrap items-center gap-1">
        <label for={id} class="font-space text-text-brand min-w-0 text-lg font-bold break-words"
          >{field.label}</label
        >
        <Tooltip.Provider delayDuration={0}>
          <Tooltip.Root bind:open={sourceOpen} disableCloseOnTriggerClick>
            <Tooltip.Trigger
              type="button"
              aria-label={`${active.source.locked ? 'Locked: ' : !editable ? 'Read only: ' : ''}Source information for ${field.label}`}
              onclick={() => (sourceOpen = true)}
              class="text-text-brand-muted hover:text-text-brand inline-flex h-8 shrink-0 items-center justify-center gap-1.5 rounded-sm {active
                .source.locked || !editable
                ? 'px-1 text-xs'
                : 'w-8'}"
            >
              {#if active.source.locked}
                <Lock class="size-3.5" aria-hidden="true" />
                Locked
              {:else if !editable}
                <Eye class="size-3.5" aria-hidden="true" />
                Read only
              {:else}
                <Info class="size-4" aria-hidden="true" />
              {/if}
            </Tooltip.Trigger>
            <Tooltip.Content
              role="tooltip"
              side="bottom"
              align="start"
              sideOffset={6}
              class="max-w-[min(22rem,calc(100vw-2rem))] space-y-1 text-left text-wrap break-words"
            >
              <p>
                Source: {sourceLabel(active.source)}{active.source.name
                  ? ` (${active.source.name})`
                  : ''}
              </p>
              {#if active.reason}<p>{active.reason}</p>{/if}
              {#if active.source.locked && active.source.web_selected}<p>
                  A stored web selection is overridden by this source.
                </p>{/if}
            </Tooltip.Content>
          </Tooltip.Root>
        </Tooltip.Provider>
      </div>
      {#if edit}
        <div class="ml-auto flex flex-wrap justify-end gap-2">
          <Badge
            variant="outline"
            class="border-hister-indigo/30 bg-hister-indigo/10 text-text-brand gap-1.5"
          >
            <Pencil aria-hidden="true" />
            Unsaved
          </Badge>
        </div>
      {/if}
    </div>
    <p id={`${id}-description`} class="text-text-brand-muted text-sm">{field.description}</p>
  </div>
  <div class="min-w-0 space-y-3 break-words">
    <div class="flex lg:justify-end">
      {#if field.schema.sensitivity !== 'none'}
        <p {id} class="text-text-brand min-w-0 text-sm">
          {active.secret_present ? 'Value stored (hidden)' : 'No stored value'}
        </p>
      {:else if field.schema.control === 'switch'}
        <Switch
          {id}
          checked={value === true}
          onCheckedChange={set}
          disabled={disabled || !editable}
          aria-describedby={description}
          aria-invalid={errors.length > 0}
        />
      {:else if field.schema.control === 'select'}
        <select
          {id}
          value={String(value ?? '')}
          onchange={(event) => set(event.currentTarget.value)}
          disabled={disabled || !editable}
          aria-describedby={description}
          aria-invalid={errors.length > 0}
          class={controlClass}
        >
          {#each field.schema.choices ?? [] as choice}<option value={choice}
              >{choice || 'Default'}</option
            >{/each}
        </select>
      {:else if field.schema.control === 'number'}
        <Input
          {id}
          class={`${controlClass} max-w-40`}
          type="number"
          value={typeof value === 'number' ? value : undefined}
          min={field.schema.min}
          max={field.schema.max}
          step="1"
          oninput={(event) =>
            set(event.currentTarget.value === '' ? null : Number(event.currentTarget.value))}
          disabled={disabled || !editable}
          aria-describedby={description}
          aria-invalid={errors.length > 0}
        />
      {:else if field.schema.control === 'text'}
        <Input
          {id}
          class={controlClass}
          value={String(value ?? '')}
          maxlength={field.schema.max_length}
          oninput={(event) => set(event.currentTarget.value)}
          disabled={disabled || !editable}
          aria-describedby={description}
          aria-invalid={errors.length > 0}
        />
      {:else}
        <p {id} class="text-text-brand min-w-0 text-sm">{display(active.value)}</p>
      {/if}
    </div>
    {#if edit}
      <p id={`${id}-status`} class="text-text-brand-muted text-xs break-words">
        Current value: {display(active.value)}. {edit.action === 'reset'
          ? 'Reset to the inherited value on save.'
          : 'Unsaved change.'}
      </p>
    {/if}
    {#if errors.length}<div id={`${id}-errors`} class="text-hister-rose text-sm">
        {#each errors as error}<p>{error}</p>{/each}
      </div>{/if}
    {#if editable}
      <div class="flex flex-wrap gap-2 lg:justify-end">
        {#if field.schema.type === 'string' && (!field.schema.choices || field.schema.choices.includes(''))}
          <Button
            type="button"
            variant="outline"
            size="sm"
            {disabled}
            onclick={() => onEdit({ path: field.path, action: 'set', value: '' })}
            >Clear {field.label}</Button
          >
        {/if}
        {#if active.source.web_selected}
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={disabled || edit?.action === 'reset'}
            onclick={() => onEdit({ path: field.path, action: 'reset' })}
            >Reset {field.label}</Button
          >
        {/if}
        {#if edit}<Button
            type="button"
            variant="ghost"
            size="sm"
            {disabled}
            onclick={() => onEdit(undefined)}>Undo {field.label}</Button
          >{/if}
      </div>
    {/if}
  </div>
</section>
