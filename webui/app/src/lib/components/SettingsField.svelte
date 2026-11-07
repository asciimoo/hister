<script lang="ts">
  import { Badge } from '@hister/components/ui/badge';
  import { Button } from '@hister/components/ui/button';
  import SettingInput from './SettingInput.svelte';
  import * as Tooltip from '@hister/components/ui/tooltip';
  import { Check, Eye, Folder, Info, Lock, Minus, Pencil } from '@lucide/svelte';
  import {
    canEditSetting,
    choiceLabel,
    sourceLabel,
    type SettingField,
    type SettingValue,
    type SettingEdit,
  } from '#lib/settings.js';

  let {
    field,
    active,
    edit,
    conflict,
    errors = [],
    disabled = false,
    preview = false,
    onEdit,
  }: {
    field: SettingField;
    active: SettingValue;
    edit?: SettingEdit;
    conflict?: string;
    errors?: string[];
    disabled?: boolean;
    preview?: boolean;
    onEdit: (edit: SettingEdit | undefined) => void;
  } = $props();
  let sourceOpen = $state(false);
  const editable = $derived(!preview && canEditSetting(field, active));
  const directories = $derived(
    !editable &&
      field.path === 'indexer.directories' &&
      field.schema.sensitivity === 'none' &&
      (Array.isArray(active.value) || active.value === null)
      ? ((active.value ?? []) as Record<string, unknown>[])
      : undefined,
  );
  const value = $derived(
    edit?.action === 'set' ? edit.value : edit?.action === 'reset' ? field.default : active.value,
  );
  const id = $derived(`${preview ? 'pending' : 'setting'}-${field.path}`);
  const description = $derived(
    `${id}-description${edit ? ` ${id}-status` : ''}${errors.length ? ` ${id}-errors` : ''}${conflict ? ` ${id}-conflict` : ''}`,
  );
  const actionClass = 'text-text-brand-muted hover:text-text-brand h-8 px-1.5 text-xs font-normal';
  const directoryEmptyValues: Record<string, string> = {
    label: 'None',
    user: 'Everyone with search access',
    filetypes: 'All file types',
    patterns: 'All file names',
    excludes: 'None',
  };
  function set(value: unknown) {
    onEdit(
      JSON.stringify(value) === JSON.stringify(active.value)
        ? undefined
        : { path: field.path, action: 'set', value },
    );
  }
  function display(value: unknown): string {
    if (value === undefined || value === null) return 'Not available';
    if (value === '') return 'Empty';
    if (typeof value === 'boolean') return value ? 'Enabled' : 'Disabled';
    const items = field.schema.items;
    if (Array.isArray(value) && items?.choices)
      return value.map((item) => choiceLabel(items, String(item))).join(', ') || 'Empty';
    if (typeof value === 'string') return choiceLabel(field.schema, value);
    return typeof value === 'object' ? JSON.stringify(value) : String(value);
  }
</script>

<section
  aria-label={preview ? `Pending ${field.label}` : field.label}
  class="border-border-brand-muted grid gap-x-8 gap-y-3 border-b px-4 py-4 last:border-b-0 md:px-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,24rem)] {edit
    ? 'bg-hister-indigo/5'
    : ''}"
>
  <div class="min-w-0 space-y-1 break-words {directories ? 'lg:col-span-2' : ''}">
    {#if preview}<p class="text-text-brand-muted text-xs font-medium">
        Configured on disk, not applied
      </p>{/if}
    <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
      <div class="flex min-w-0 flex-wrap items-center gap-1">
        <svelte:element
          this={editable ? 'label' : 'span'}
          for={editable ? id : undefined}
          class="font-inter text-text-brand min-w-0 text-sm font-medium break-words"
          >{field.label}</svelte:element
        >
        <Tooltip.Provider delayDuration={0}>
          <Tooltip.Root bind:open={sourceOpen} disableCloseOnTriggerClick>
            <Tooltip.Trigger
              type="button"
              aria-label={`${active.source.locked ? 'Locked: ' : !editable ? 'Read only: ' : ''}Source information for ${field.label}`}
              onclick={() => (sourceOpen = true)}
              class="text-text-brand-muted hover:text-text-brand inline-flex h-8 shrink-0 cursor-pointer items-center justify-center gap-1.5 rounded-sm {active
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
    <p id={`${id}-description`} class="text-text-brand-muted text-sm leading-relaxed">
      {field.description}
    </p>
  </div>
  <div class="min-w-0 space-y-2 break-words {directories ? 'lg:col-span-2' : ''}">
    <div class="flex min-h-8 items-center lg:justify-end">
      {#if field.schema.sensitivity !== 'none'}
        <p {id} class="text-text-brand min-w-0 text-sm">
          {active.secret_present ? 'Value stored (hidden)' : 'No stored value'}
        </p>
      {:else if directories}
        <div {id} class="w-full min-w-0 space-y-3" aria-describedby={description}>
          {#each directories as directory, index}
            <div
              role="group"
              aria-label={`${field.label} ${index + 1}`}
              class="border-border-brand-muted min-w-0 border"
            >
              <div class="bg-muted/30 flex min-w-0 items-start gap-3 p-4">
                <Folder class="text-text-brand-muted mt-0.5 size-4 shrink-0" aria-hidden="true" />
                <h3
                  class="text-text-brand min-w-0 font-mono text-sm font-medium [overflow-wrap:anywhere]"
                >
                  {String(directory.path ?? '')}
                </h3>
              </div>
              <dl class="grid min-w-0 gap-x-8 gap-y-4 p-4 sm:grid-cols-2">
                {#each field.schema.items?.properties ?? [] as property (property.name)}
                  {#if property.name !== 'path'}
                    {@const member = directory[property.name]}
                    <div class="min-w-0 space-y-1">
                      <dt class="text-text-brand-muted text-xs">{property.label}</dt>
                      <dd class="text-text-brand min-w-0 text-sm [overflow-wrap:anywhere]">
                        {#if property.schema.sensitivity !== 'none'}
                          Value hidden
                        {:else if property.schema.type === 'boolean'}
                          <span class="inline-flex items-center gap-1.5">
                            {#if member === true}
                              <Check class="size-3.5" aria-hidden="true" />Enabled
                            {:else}
                              <Minus
                                class="text-text-brand-muted size-3.5"
                                aria-hidden="true"
                              />Disabled
                            {/if}
                          </span>
                        {:else if member == null || member === '' || (Array.isArray(member) && member.length === 0)}
                          <span class="text-text-brand-muted">
                            {directoryEmptyValues[property.name] ?? 'None'}
                          </span>
                        {:else if Array.isArray(member)}
                          <div class="flex min-w-0 flex-wrap gap-1.5">
                            {#each member as item}
                              <Badge
                                variant="outline"
                                class="border-border-brand-muted text-text-brand max-w-full font-mono font-normal whitespace-normal"
                              >
                                {String(item)}
                              </Badge>{' '}
                            {/each}
                          </div>
                        {:else}
                          {display(member)}
                        {/if}
                      </dd>
                    </div>
                  {/if}
                {/each}
              </dl>
            </div>
          {:else}
            <p class="text-text-brand-muted text-sm">No watched directories configured.</p>
          {/each}
        </div>
      {:else if !editable}
        <p {id} class="text-text-brand min-w-0 text-sm">{display(active.value)}</p>
      {:else}
        <SettingInput
          {id}
          label={field.label}
          schema={field.schema}
          {value}
          {disabled}
          {description}
          invalid={errors.length > 0}
          onChange={set}
        />
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
    {#if conflict && edit}
      <div
        id={`${id}-conflict`}
        role="alert"
        class="border-hister-rose/40 space-y-2 border p-3 text-sm"
      >
        <p>{conflict}</p>
        <p class="text-text-brand-muted [overflow-wrap:anywhere]">
          Your draft: {edit.action === 'reset'
            ? 'Reset to the inherited value'
            : field.schema.sensitivity === 'none'
              ? display(edit.value)
              : 'Value hidden'}
        </p>
        <div class="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            {disabled}
            onclick={() => onEdit(undefined)}>Use current value</Button
          >
          {#if editable}<Button
              type="button"
              variant="outline"
              size="sm"
              {disabled}
              onclick={() => onEdit(edit)}>Keep my edit</Button
            >{/if}
        </div>
      </div>
    {/if}
    {#if editable}
      <div class="flex flex-wrap gap-2 lg:justify-end">
        {#if field.schema.type === 'list' || (field.schema.type === 'string' && (!field.schema.choices || field.schema.choices.includes('')))}
          <Button
            type="button"
            variant="link"
            size="sm"
            class={actionClass}
            aria-label={`Clear ${field.label}`}
            {disabled}
            onclick={() =>
              onEdit({
                path: field.path,
                action: 'set',
                value: field.schema.type === 'list' ? [] : '',
              })}>Clear</Button
          >
        {/if}
        {#if active.source.web_selected}
          <Button
            type="button"
            variant="link"
            size="sm"
            class={actionClass}
            aria-label={`Reset ${field.label}`}
            disabled={disabled || edit?.action === 'reset'}
            onclick={() => onEdit({ path: field.path, action: 'reset' })}>Reset</Button
          >
        {/if}
        {#if edit}<Button
            type="button"
            variant="link"
            size="sm"
            class={actionClass}
            aria-label={`Undo ${field.label}`}
            {disabled}
            onclick={() => onEdit(undefined)}>Undo</Button
          >{/if}
      </div>
    {/if}
  </div>
</section>
