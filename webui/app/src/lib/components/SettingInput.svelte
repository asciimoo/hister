<script lang="ts">
  import { Button } from '@hister/components/ui/button';
  import { Input } from '@hister/components/ui/input';
  import { Switch } from '@hister/components/ui/switch';
  import { choiceLabel, type SettingSchema } from '#lib/settings.js';
  import SettingInput from './SettingInput.svelte';

  let {
    id,
    label,
    schema,
    value,
    disabled = false,
    description,
    invalid = false,
    onChange,
  }: {
    id: string;
    label: string;
    schema: SettingSchema;
    value: unknown;
    disabled?: boolean;
    description?: string;
    invalid?: boolean;
    onChange: (value: unknown) => void;
  } = $props();
  const items = $derived(Array.isArray(value) ? value : []);
  const record = $derived(
    value && typeof value === 'object' && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : {},
  );
  const controlClass =
    'border-input bg-background text-text-brand focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 h-10 w-full min-w-0 border px-3 py-2 text-base transition-[color,box-shadow] outline-none focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50 md:text-sm';

  function empty(schema: SettingSchema): unknown {
    if (schema.type === 'object')
      return Object.fromEntries(
        (schema.properties ?? []).map((property) => [property.name, empty(property.schema)]),
      );
    if (schema.type === 'list') return [];
    if (schema.type === 'boolean') return false;
    if (schema.type === 'integer') return schema.min ?? 0;
    return '';
  }
</script>

{#if schema.control === 'switch'}
  <Switch
    {id}
    class="cursor-pointer"
    checked={value === true}
    onCheckedChange={onChange}
    {disabled}
    aria-describedby={description}
    aria-invalid={invalid}
  />
{:else if schema.control === 'select'}
  <select
    {id}
    value={String(value ?? '')}
    onchange={(event) => onChange(event.currentTarget.value)}
    {disabled}
    aria-describedby={description}
    aria-invalid={invalid}
    class={`${controlClass} cursor-pointer`}
  >
    {#each schema.choices ?? [] as choice}<option value={choice}
        >{choiceLabel(schema, choice) || 'Default'}</option
      >{/each}
  </select>
{:else if schema.control === 'number'}
  <Input
    {id}
    class={`${controlClass} max-w-40 cursor-text`}
    type="number"
    value={typeof value === 'number' ? value : undefined}
    min={schema.min}
    max={schema.max}
    step="1"
    oninput={(event) =>
      onChange(event.currentTarget.value === '' ? null : Number(event.currentTarget.value))}
    {disabled}
    aria-describedby={description}
    aria-invalid={invalid}
  />
{:else if schema.control === 'text'}
  <Input
    {id}
    class={`${controlClass} cursor-text`}
    value={String(value ?? '')}
    maxlength={schema.max_length}
    oninput={(event) => onChange(event.currentTarget.value)}
    {disabled}
    aria-describedby={description}
    aria-invalid={invalid}
  />
{:else if schema.type === 'list' && schema.items?.choices}
  <select
    {id}
    multiple
    size={Math.min(8, schema.items.choices.length)}
    value={items}
    onchange={(event) =>
      onChange(Array.from(event.currentTarget.selectedOptions, (option) => option.value))}
    {disabled}
    aria-describedby={description}
    aria-invalid={invalid}
    class={`${controlClass} h-auto cursor-pointer`}
  >
    {#each schema.items.choices as choice}<option value={choice}
        >{choiceLabel(schema.items, choice)}</option
      >{/each}
  </select>
{:else if schema.type === 'list' && schema.items?.type === 'string'}
  <textarea
    {id}
    value={items.join('\n')}
    oninput={(event) =>
      onChange(event.currentTarget.value === '' ? [] : event.currentTarget.value.split('\n'))}
    {disabled}
    aria-describedby={description}
    aria-invalid={invalid}
    rows="3"
    class={`${controlClass} h-auto resize-y`}></textarea>
{:else if schema.type === 'list' && schema.items}
  <div
    {id}
    role="group"
    aria-label={label}
    aria-describedby={description}
    class="w-full min-w-0 space-y-3"
  >
    {#each items as item, index}
      <fieldset
        aria-label={`${label} ${index + 1}`}
        {disabled}
        class="border-border-brand-muted min-w-0 space-y-3 border p-3"
      >
        <legend class="px-1 text-sm font-medium">{label} {index + 1}</legend>
        <SettingInput
          id={`${id}-${index}`}
          label={`${label} ${index + 1}`}
          schema={schema.items}
          value={item}
          {disabled}
          {invalid}
          onChange={(value) => onChange(items.map((item, i) => (i === index ? value : item)))}
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          {disabled}
          aria-label={`Remove ${label} item ${index + 1}`}
          onclick={() => onChange(items.filter((_, i) => i !== index))}>Remove</Button
        >
      </fieldset>
    {/each}
    <Button
      type="button"
      variant="outline"
      size="sm"
      disabled={disabled || (!!schema.max_items && items.length >= schema.max_items)}
      aria-label={`Add ${label} item`}
      onclick={() => onChange([...items, empty(schema.items!)])}>Add item</Button
    >
  </div>
{:else if schema.type === 'object'}
  <div class="w-full min-w-0 space-y-3">
    {#each schema.properties ?? [] as property (property.name)}
      {@const memberID = `${id}-${property.name}`}
      <div class="min-w-0 space-y-1">
        <div class="flex flex-wrap items-baseline gap-x-2">
          <label for={memberID} class="text-sm font-medium">{property.label}</label>
          <span id={`${memberID}-requirement`} class="text-text-brand-muted text-xs">
            {property.required ? 'Required' : 'Optional'}
          </span>
        </div>
        {#if property.description}<p
            id={`${memberID}-description`}
            class="text-text-brand-muted text-xs"
          >
            {property.description}
          </p>{/if}
        <SettingInput
          id={memberID}
          label={property.label}
          schema={property.schema}
          value={record[property.name]}
          {disabled}
          {invalid}
          description={`${memberID}-requirement${property.description ? ` ${memberID}-description` : ''}`}
          onChange={(value) => onChange({ ...record, [property.name]: value })}
        />
      </div>
    {/each}
  </div>
{/if}
