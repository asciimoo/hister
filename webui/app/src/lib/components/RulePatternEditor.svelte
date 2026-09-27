<script lang="ts">
  import { previewRule, type RuleMatchMode, type RulePreview } from '$lib/rules';
  import { Input } from '@hister/components/ui/input';
  import { Label } from '@hister/components/ui/label';
  import { Button } from '@hister/components/ui/button';
  import { Check, X } from '@lucide/svelte';

  interface Props {
    id: string;
    pattern: string;
    valid: boolean;
    mode?: RuleMatchMode;
    disabled?: boolean;
    onSubmit?: () => void | Promise<void>;
    onCancel?: () => void;
  }

  let {
    id,
    pattern = $bindable(),
    valid = $bindable(),
    mode = 'regex',
    disabled = false,
    onSubmit,
    onCancel,
  }: Props = $props();

  let domain = $state('');
  let exactURL = $state('');
  let includeSubdomains = $state(true);
  let testURL = $state('');
  let checking = $state(false);
  let error = $state('');
  let preview = $state<RulePreview | null>(null);
  let retry = $state(0);

  function handleKeydown(event: KeyboardEvent) {
    if (event.isComposing) return;
    if (event.key === 'Enter' && onSubmit) {
      event.preventDefault();
      if (valid && !disabled) void onSubmit();
    } else if (event.key === 'Escape' && onCancel) {
      event.preventDefault();
      onCancel();
    }
  }

  $effect(() => {
    const value = (mode === 'domain' ? domain : mode === 'url' ? exactURL : pattern).trim();
    const draft =
      mode === 'domain'
        ? { domain: value, include_subdomains: includeSubdomains }
        : mode === 'url'
          ? { exact_url: value }
          : { pattern: value };
    const url = testURL.trim();
    const generatedMode = mode !== 'regex';
    void retry;
    valid = false;
    preview = null;
    error = '';
    checking = !!value;
    if (generatedMode) pattern = '';
    if (!value) return;

    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      try {
        const result = await previewRule({ ...draft, url }, controller.signal);
        if (controller.signal.aborted) return;
        preview = result;
        if (generatedMode) pattern = result.pattern;
        valid = true;
      } catch (e) {
        if (!controller.signal.aborted) {
          error = e instanceof Error ? e.message : String(e);
        }
      } finally {
        if (!controller.signal.aborted) checking = false;
      }
    }, 300);

    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  });
</script>

<div class="min-w-0 space-y-3">
  <div class="space-y-1.5">
    <Label for={`${id}-pattern`} class="font-outfit text-text-brand text-sm font-bold">
      {mode === 'domain' ? 'Domain' : mode === 'url' ? 'URL' : 'Regexp pattern'}
    </Label>
    {#if mode === 'domain'}
      <Input
        id={`${id}-pattern`}
        variant="brutal"
        bind:value={domain}
        onkeydown={handleKeydown}
        {disabled}
        placeholder="example.com"
        autocomplete="off"
        spellcheck={false}
        aria-describedby={`${id}-help`}
        class="bg-card-surface focus-visible:border-hister-coral h-10 w-full px-3"
      />
      <p id={`${id}-help`} class="font-inter text-text-brand-muted text-xs">
        Enter a domain or paste a page URL. Matches pages on that domain over HTTP or HTTPS, on any
        port.
      </p>
      <label class="font-inter text-text-brand-secondary flex items-center gap-2 py-1 text-sm">
        <input
          type="checkbox"
          bind:checked={includeSubdomains}
          {disabled}
          class="accent-hister-coral size-4"
        />
        Include subdomains
      </label>
    {:else if mode === 'url'}
      <Input
        id={`${id}-pattern`}
        type="url"
        variant="brutal"
        bind:value={exactURL}
        onkeydown={handleKeydown}
        {disabled}
        placeholder="https://example.com/page"
        autocomplete="off"
        spellcheck={false}
        aria-describedby={`${id}-help`}
        class="bg-card-surface focus-visible:border-hister-coral h-10 w-full px-3"
      />
      <p id={`${id}-help`} class="font-inter text-text-brand-muted text-xs">
        Matches only this complete URL, including its path and query. Use the URL shown in your
        index.
      </p>
    {:else}
      <Input
        id={`${id}-pattern`}
        variant="brutal"
        bind:value={pattern}
        onkeydown={handleKeydown}
        {disabled}
        placeholder={'^https://example\\.com/'}
        autocomplete="off"
        spellcheck={false}
        aria-describedby={`${id}-help`}
        class="bg-card-surface font-fira focus-visible:border-hister-coral h-10 w-full px-3 text-sm"
      />
      <p id={`${id}-help`} class="font-inter text-text-brand-muted text-xs">
        One <a
          href="https://pkg.go.dev/regexp/syntax"
          target="_blank"
          rel="noopener noreferrer"
          class="underline underline-offset-2">Go regexp pattern</a
        >
        matched against the full URL. Use <code>\s</code> to match whitespace.
      </p>
    {/if}
  </div>

  <div class="space-y-1.5">
    <Label for={`${id}-test-url`} class="font-outfit text-text-brand text-sm font-bold">
      Test URL <span class="font-inter text-text-brand-muted font-normal">(optional)</span>
    </Label>
    <Input
      id={`${id}-test-url`}
      type="text"
      inputmode="url"
      variant="brutal"
      bind:value={testURL}
      onkeydown={handleKeydown}
      {disabled}
      placeholder="https://example.com/page"
      autocomplete="off"
      spellcheck={false}
      aria-describedby={`${id}-test-help ${id}-status`}
      class="bg-card-surface focus-visible:border-hister-coral h-10 w-full px-3"
    />
    <p id={`${id}-test-help`} class="font-inter text-text-brand-muted text-xs">
      Checks this draft only, without saving it or opening the URL. Other saved rules can affect
      indexing.
    </p>
  </div>

  <div id={`${id}-status`} role="status" aria-live="polite" class="font-inter text-sm">
    {#if checking}
      <p class="text-text-brand-muted">Checking rule…</p>
    {:else if error}
      <div class="text-hister-rose flex flex-wrap items-center gap-2">
        <p>{error}</p>
        <Button variant="outline" size="sm" {disabled} onclick={() => retry++}>Retry</Button>
      </div>
    {:else if preview?.matches !== undefined}
      <p class="text-text-brand flex items-center gap-2">
        {#if preview.matches}
          <Check class="text-hister-teal size-4 shrink-0" aria-hidden="true" />
          This URL matches the rule.
        {:else}
          <X class="text-text-brand-muted size-4 shrink-0" aria-hidden="true" />
          This URL does not match the rule.
        {/if}
      </p>
    {:else if preview}
      <p class="text-text-brand-secondary">Rule is valid. Enter a URL to test it.</p>
    {/if}
  </div>

  {#if mode !== 'regex' && preview}
    <details class="text-text-brand-muted text-xs">
      <summary class="font-inter cursor-pointer">Generated regexp</summary>
      <code class="font-fira mt-2 block break-all">{preview.pattern}</code>
    </details>
  {/if}
</div>
