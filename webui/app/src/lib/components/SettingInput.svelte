<script lang="ts">
  import { Button } from '@hister/components/ui/button';
  import { Input } from '@hister/components/ui/input';
  import { Switch } from '@hister/components/ui/switch';
  import { choiceLabel, emptySettingValue as empty, type SettingSchema } from '#lib/settings.js';
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
  const hasStoredSecret = $derived(typeof record.ref === 'string' && record.ref !== '');
  const controlClass =
    'border-input bg-background text-text-brand focus-visible:border-ring focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 h-10 w-full min-w-0 border px-3 py-2 text-base transition-[color,box-shadow] outline-none focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50 md:text-sm';
  let dictionaryError = $state('');

  function secretAction(action: string) {
    onChange({
      action,
      ...(record.ref ? { ref: record.ref } : {}),
      ...(action === 'replace' ? { value: '' } : {}),
    });
  }

  function renameEntry(previous: string, next: string) {
    if (
      previous !== next &&
      Object.keys(record).some((name) => name.toLowerCase() === next.toLowerCase())
    ) {
      dictionaryError = 'Names must be unique. The existing entry has been kept.';
      return false;
    }
    dictionaryError = '';
    onChange(
      Object.fromEntries(
        Object.entries(record).map(([name, value]) => [name === previous ? next : name, value]),
      ),
    );
    return true;
  }
</script>

{#if schema.control === 'secret' || (schema.control === 'url' && schema.sensitivity === 'url')}
  <div class="w-full min-w-0 space-y-2">
    {#if hasStoredSecret}
      <select
        {id}
        aria-label={`${label} action`}
        value={String(record.action)}
        onchange={(event) => secretAction(event.currentTarget.value)}
        {disabled}
        aria-describedby={description}
        class={controlClass}
      >
        <option value="keep">Keep stored value</option>
        <option value="replace">Replace</option>
        {#if !schema.min_length}<option value="clear">Clear</option>{/if}
      </select>
    {/if}
    {#if !hasStoredSecret || record.action === 'replace'}
      <Input
        id={hasStoredSecret ? `${id}-replacement` : id}
        type="password"
        aria-label={hasStoredSecret ? `${label} replacement` : label}
        autocomplete="new-password"
        value={String(record.value ?? '')}
        maxlength={schema.max_length}
        minlength={schema.min_length}
        oninput={(event) =>
          onChange({ ...record, action: 'replace', value: event.currentTarget.value })}
        {disabled}
        aria-invalid={invalid}
        aria-describedby={description}
        class={controlClass}
      />
    {:else}
      <p class="text-text-brand-muted text-xs">
        {record.action === 'keep' ? 'Stored value stays hidden.' : 'Empty value selected.'}
      </p>
    {/if}
  </div>
{:else if schema.control === 'switch'}
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
        >{choiceLabel(schema, choice)}</option
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
    step={schema.type === 'number' ? 'any' : '1'}
    oninput={(event) =>
      onChange(event.currentTarget.value === '' ? null : Number(event.currentTarget.value))}
    {disabled}
    aria-describedby={description}
    aria-invalid={invalid}
  />
{:else if schema.control === 'text' || schema.control === 'url'}
  <Input
    {id}
    class={`${controlClass} cursor-text`}
    type={schema.control === 'url' ? 'url' : 'text'}
    value={String(value ?? '')}
    maxlength={schema.max_length}
    minlength={schema.min_length}
    oninput={(event) => onChange(event.currentTarget.value)}
    {disabled}
    aria-describedby={description}
    aria-invalid={invalid}
  />
{:else if schema.type === 'dictionary' && schema.items}
  <div
    {id}
    role="group"
    aria-label={label}
    aria-describedby={description}
    class="w-full min-w-0 space-y-3"
  >
    {#each Object.entries(record) as [name, item], index}
      <fieldset
        aria-label={`${label} ${index + 1}`}
        {disabled}
        class="border-border-brand-muted min-w-0 space-y-3 border p-3"
      >
        <legend class="px-1 text-sm font-medium">{label} {index + 1}</legend>
        <label class="block text-sm" for={`${id}-${index}-name`}>Name</label>
        <Input
          id={`${id}-${index}-name`}
          value={name}
          {disabled}
          oninput={(event) => {
            if (!renameEntry(name, event.currentTarget.value)) event.currentTarget.value = name;
          }}
          class={controlClass}
        />
        <label class="block text-sm" for={`${id}-${index}-value`}>Value</label>
        <SettingInput
          id={`${id}-${index}-value`}
          label="Value"
          schema={schema.items}
          value={item}
          {disabled}
          {invalid}
          onChange={(value) => onChange({ ...record, [name]: value })}
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          {disabled}
          aria-label={`Remove ${label} item ${index + 1}`}
          onclick={() =>
            onChange(Object.fromEntries(Object.entries(record).filter(([key]) => key !== name)))}
          >Remove</Button
        >
      </fieldset>
    {/each}
    {#if dictionaryError}<p role="alert" class="text-hister-rose text-sm">{dictionaryError}</p>{/if}
    <Button
      type="button"
      variant="outline"
      size="sm"
      disabled={disabled ||
        Object.hasOwn(record, '') ||
        (!!schema.max_items && Object.keys(record).length >= schema.max_items)}
      aria-label={`Add ${label} item`}
      onclick={() => onChange({ ...record, '': empty(schema.items!) })}>Add item</Button
    >
  </div>
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
        {#if schema.collection === 'replace' && Object.hasOwn(record, property.name)}
          <Button
            type="button"
            variant="link"
            size="sm"
            {disabled}
            aria-label={`Remove ${property.label}`}
            onclick={() =>
              onChange(
                Object.fromEntries(
                  Object.entries(record).filter(([name]) => name !== property.name),
                ),
              )}>Remove selection</Button
          >
        {/if}
      </div>
    {/each}
  </div>
{/if}
