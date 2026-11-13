<script lang="ts">
  import { Button } from '@hister/components/ui/button';
  import { Input } from '@hister/components/ui/input';
  import { Badge } from '@hister/components/ui/badge';
  import { Kbd } from '@hister/components/ui/kbd';
  import * as Tooltip from '@hister/components/ui/tooltip';
  import { Pencil, Plus, Trash2 } from '@lucide/svelte';
  import {
    choiceLabel,
    emptySettingValue,
    type SettingField,
    type SettingItemEditor,
    type SettingRemoval,
  } from '#lib/settings.js';
  import SettingInput from './SettingInput.svelte';

  let {
    id,
    field,
    value,
    activeValue,
    editor,
    removal,
    disabled,
    blocked,
    onEditor,
    onDone,
    onRemove,
    onUndoRemoval,
  }: {
    id: string;
    field: SettingField;
    value: unknown;
    activeValue: unknown;
    editor: SettingItemEditor | null;
    removal: SettingRemoval | null;
    disabled: boolean;
    blocked: boolean;
    onEditor: (editor: SettingItemEditor | null) => void;
    onDone: (value: unknown) => Promise<void>;
    onRemove: (value: unknown, previous: unknown) => void;
    onUndoRemoval: () => void;
  } = $props();
  const dictionary = $derived(field.schema.type === 'dictionary');
  const schema = $derived(field.schema.items!);
  const entries = $derived(toEntries(value));
  const activeEntries = $derived(toEntries(activeValue));
  const current = $derived(editor?.path === field.path ? editor : null);
  const noun = $derived(
    (
      {
        'crawler.cookies': 'cookie',
        'crawler.headers': 'header',
        'semantic_search.headers': 'header',
        'indexer.directories': 'directory',
        'hotkeys.web': 'shortcut',
        sensitive_content_patterns: 'pattern',
      } as Record<string, string>
    )[field.path] ?? 'item',
  );
  const directory = $derived(field.path === 'indexer.directories');
  const shortcut = $derived(field.path === 'hotkeys.web');
  const pattern = $derived(field.path === 'sensitive_content_patterns');
  const keyLabel = $derived(field.schema.key_label ?? 'Name');
  const valueLabel = $derived(field.schema.value_label ?? 'Value');
  const primarySchema = $derived({
    ...schema,
    properties: schema.properties?.filter((property) =>
      ['path', 'label', 'user'].includes(property.name),
    ),
  });
  const filtersSchema = $derived({
    ...schema,
    properties: schema.properties?.filter(
      (property) => !['path', 'label', 'user'].includes(property.name),
    ),
  });
  let nameError = $state('');

  function toEntries(value: unknown): [string, unknown][] {
    if (dictionary) return Object.entries((value ?? {}) as Record<string, unknown>);
    return (Array.isArray(value) ? value : []).map((item) => ['', item]);
  }

  function collection(entries: [string, unknown][]) {
    return dictionary ? Object.fromEntries(entries) : entries.map(([, item]) => item);
  }

  function start(index: number | null) {
    nameError = '';
    onEditor({
      path: field.path,
      index,
      name: index === null ? '' : entries[index][0],
      value: index === null ? emptySettingValue(schema) : entries[index][1],
    });
  }

  async function done() {
    if (!current || disabled || blocked) return;
    if (dictionary) {
      if (!current.name.trim()) {
        nameError = 'Enter a name.';
        return;
      }
      if (
        entries.some(
          ([name], index) =>
            index !== current.index && name.toLowerCase() === current.name.toLowerCase(),
        )
      ) {
        nameError = 'Names must be unique. Choose a different name.';
        return;
      }
    }
    nameError = '';
    const next = [...entries];
    const entry: [string, unknown] = [current.name, current.value];
    if (current.index === null) next.push(entry);
    else next[current.index] = entry;
    await onDone(collection(next));
  }

  function summary(name: string, value: unknown) {
    const item = value as Record<string, unknown>;
    if (dictionary)
      return {
        title: name,
        detail:
          schema.sensitivity === 'none'
            ? choiceLabel(schema, String(value ?? ''))
            : secretSummary(value),
      };
    if (directory)
      return {
        title: String(item.path || 'Directory'),
        detail: [
          Array.isArray(item.filetypes) && item.filetypes.length
            ? item.filetypes.join(', ')
            : 'All file types',
          item.user ? `Owner: ${item.user}` : 'Everyone with search access',
          item.label ? `Label: ${item.label}` : '',
        ]
          .filter(Boolean)
          .join(' · '),
      };
    return {
      title: String(item.name || 'Cookie'),
      detail: `${item.domain ?? ''} · ${item.path || '/'} · ${secretSummary(item.value)}`,
    };
  }

  function secretSummary(value: unknown) {
    const secret = value as { action?: string; value?: string } | undefined;
    if (secret?.action === 'keep') return 'Value stored';
    if (secret?.action === 'replace' && secret.value) return 'New value';
    return 'Empty value';
  }

  function focusEditor(node: HTMLElement) {
    queueMicrotask(() => node.querySelector<HTMLElement>('input, select, textarea')?.focus());
  }
</script>

{#snippet itemEditor(index: number)}
  {#if current}
    <fieldset
      aria-label={`${field.label} ${index + 1}`}
      class="border-hister-indigo/40 min-w-0 space-y-4 border p-4"
      use:focusEditor
    >
      <legend class="px-1 text-sm font-medium"
        >{current.index === null ? 'New' : 'Edit'} {noun}</legend
      >
      {#if dictionary}
        <label class="block text-sm" for={`${id}-${index}-name`}>{keyLabel}</label>
        <Input
          id={`${id}-${index}-name`}
          value={current.name}
          {disabled}
          aria-invalid={!!nameError}
          aria-describedby={nameError ? `${id}-name-error` : undefined}
          oninput={(event) => {
            nameError = '';
            onEditor({ ...current!, name: event.currentTarget.value });
          }}
        />
        <label class="block text-sm" for={`${id}-${index}-value`}>{valueLabel}</label>
      {/if}
      <SettingInput
        id={`${id}-${index}${dictionary ? '-value' : ''}`}
        label={dictionary ? valueLabel : `${field.label} ${index + 1}`}
        schema={directory ? primarySchema : schema}
        value={current.value}
        {disabled}
        onChange={(value) => onEditor({ ...current!, value })}
      />
      {#if directory}
        <details class="border-border-brand-muted border-t pt-3">
          <summary class="cursor-pointer text-sm font-medium">Filters and options</summary>
          <div class="pt-4">
            <SettingInput
              id={`${id}-${index}-filters`}
              label="Filters and options"
              schema={filtersSchema}
              value={current.value}
              {disabled}
              onChange={(value) => onEditor({ ...current!, value })}
            />
          </div>
        </details>
      {/if}
      {#if nameError}<p id={`${id}-name-error`} role="alert" class="text-hister-rose text-sm">
          {nameError}
        </p>{/if}
      <div class="flex flex-wrap justify-end gap-2">
        <Button type="button" variant="outline" size="sm" {disabled} onclick={() => onEditor(null)}
          >Cancel</Button
        >
        <Button type="button" size="sm" disabled={disabled || blocked} onclick={done}>
          {current.index === null ? `Add ${noun}` : 'Done'}
        </Button>
      </div>
      <p class="text-text-brand-muted text-xs">Save and apply activates your changes.</p>
    </fieldset>
  {/if}
{/snippet}

{#snippet itemSummary(name: string, item: unknown, index: number)}
  {@const text = summary(name, item)}
  {@const changed = !activeEntries.some(
    ([key, active]) => key === name && JSON.stringify(active) === JSON.stringify(item),
  )}
  <fieldset
    aria-label={`${field.label} ${index + 1}`}
    class="min-w-0 space-y-2 border px-3 py-2 transition-colors {changed
      ? 'border-hister-indigo/40 bg-hister-indigo/5'
      : 'border-border-brand-muted bg-background/40 hover:border-border-brand'}"
  >
    <div class="flex min-w-0 items-start justify-between gap-2">
      <div class="flex min-h-10 min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-1.5 sm:min-h-9">
        {#if shortcut}
          <Kbd class="border-border-brand-muted text-text-brand h-7 border px-2 font-mono text-xs"
            >{text.title}</Kbd
          >
          <p class="text-text-brand-muted text-sm [overflow-wrap:anywhere]">{text.detail}</p>
        {:else}
          <p class="min-w-0 text-sm font-medium [overflow-wrap:anywhere]">{text.title}</p>
        {/if}
        {#if changed}
          <Badge
            variant="outline"
            class="border-hister-indigo/30 text-text-brand shrink-0 text-[10px]">Unsaved</Badge
          >
        {/if}
      </div>
      <div class="flex shrink-0 items-center gap-0.5">
        <Tooltip.Provider delayDuration={200}>
          <Tooltip.Root>
            <Tooltip.Trigger
              id={`${id}-${index}-edit`}
              disabled={disabled || !!editor || blocked}
              onclick={() => start(index)}
            >
              {#snippet child({ props })}
                <Button
                  {...props}
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Edit"
                  class="text-text-brand-muted hover:text-text-brand size-10 sm:size-9"
                >
                  <Pencil class="size-4" aria-hidden="true" />
                </Button>
              {/snippet}
            </Tooltip.Trigger>
            <Tooltip.Content role="tooltip" side="top" sideOffset={6}>Edit {noun}</Tooltip.Content>
          </Tooltip.Root>
          <Tooltip.Root>
            <Tooltip.Trigger
              disabled={disabled || !!editor}
              onclick={() =>
                onRemove(collection(entries.filter((_, position) => position !== index)), value)}
            >
              {#snippet child({ props })}
                <Button
                  {...props}
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Remove ${field.label} item ${index + 1}`}
                  class="text-text-brand-muted hover:bg-hister-rose/10 hover:text-hister-rose size-10 sm:size-9"
                >
                  <Trash2 class="size-4" aria-hidden="true" />
                </Button>
              {/snippet}
            </Tooltip.Trigger>
            <Tooltip.Content role="tooltip" side="top" sideOffset={6}
              >Remove {noun} from draft</Tooltip.Content
            >
          </Tooltip.Root>
        </Tooltip.Provider>
      </div>
    </div>
    {#if pattern}
      <code
        class="bg-muted/50 text-text-brand block min-w-0 px-2.5 py-2 font-mono text-xs leading-relaxed [overflow-wrap:anywhere] whitespace-pre-wrap"
        >{text.detail}</code
      >
    {:else if !shortcut}
      <p class="text-text-brand-muted pb-1 text-xs [overflow-wrap:anywhere]">{text.detail}</p>
    {/if}
  </fieldset>
{/snippet}

<div {id} role="group" aria-label={field.label} class="w-full min-w-0 space-y-2">
  {#each entries as [name, item], index}
    {#if current?.index === index}
      {@render itemEditor(index)}
    {:else}
      {@render itemSummary(name, item, index)}
    {/if}
  {/each}
  {#if current?.index === null}
    {@render itemEditor(entries.length)}
  {:else}
    {#if !entries.length}<p class="text-text-brand-muted text-sm">
        No {noun === 'directory' ? 'directories' : `${noun}s`} configured.
      </p>{/if}
    <Button
      id={`${id}-add`}
      type="button"
      variant="outline"
      size="sm"
      disabled={disabled ||
        !!editor ||
        blocked ||
        (field.schema.max_items !== undefined && entries.length >= field.schema.max_items)}
      onclick={() => start(null)}><Plus class="size-4" aria-hidden="true" />Add {noun}</Button
    >
  {/if}
  {#if removal?.path === field.path}
    <div class="flex flex-wrap items-center gap-2 text-sm" aria-live="polite">
      <p>Item removed from the draft.</p>
      <Button
        type="button"
        variant="link"
        size="sm"
        disabled={disabled || !!editor || blocked}
        onclick={onUndoRemoval}>Undo removal</Button
      >
    </div>
  {/if}
</div>
