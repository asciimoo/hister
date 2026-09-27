<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchConfig, apiFetch } from '$lib/api';
  import { buildPreviewUrl } from '$lib/preview';
  import { base } from '$app/paths';
  import { Input } from '@hister/components/ui/input';
  import { Textarea } from '@hister/components/ui/textarea';
  import { Label } from '@hister/components/ui/label';
  import { Button } from '@hister/components/ui/button';
  import * as Card from '@hister/components/ui/card';
  import * as Alert from '@hister/components/ui/alert';
  import AlertCircle from '@lucide/svelte/icons/circle-alert';
  import CheckCircle from '@lucide/svelte/icons/circle-check';
  import {
    Database,
    ExternalLink,
    Eye,
    FileText,
    Link,
    LoaderCircle,
    Plus,
    RotateCcw,
    Save,
    Type,
  } from '@lucide/svelte';

  let url = $state('');
  let title = $state('');
  let text = $state('');
  let message = $state('');
  let isError = $state(false);
  let submitting = $state(false);
  let urlInput: HTMLInputElement | null = $state(null);
  let addedDocument = $state<{ url: string; title: string } | null>(null);
  const contentChars = $derived(text.trim().length);
  const canSubmit = $derived(url.trim().length > 0 && !submitting);

  onMount(async () => {
    try {
      const cfg = await fetchConfig();
      if (cfg.public && !cfg.canWrite) {
        window.location.href = base + '/auth';
      }
    } catch {
      // The form can still render when the standalone dev UI is running without the API.
    }
  });

  async function handleSubmit(e: Event) {
    e.preventDefault();
    if (submitting) return;
    const submittedUrl = url.trim();
    const submittedTitle = title.trim();
    submitting = true;
    message = '';
    addedDocument = null;
    try {
      const res = await apiFetch('/add', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url: submittedUrl, title: submittedTitle, text }),
      });
      if (res.status === 201) {
        message = 'Document added successfully.';
        isError = false;
        addedDocument = { url: submittedUrl, title: submittedTitle };
        url = '';
        title = '';
        text = '';
      } else if (res.status === 406) {
        message = 'URL skipped (matches skip rules or is a local URL).';
        isError = false;
      } else {
        message = 'Failed to add document.';
        isError = true;
      }
    } catch (err) {
      message = String(err);
      isError = true;
    } finally {
      submitting = false;
    }
  }

  function clearForm() {
    url = '';
    title = '';
    text = '';
    message = '';
    addedDocument = null;
  }

  function addAnother() {
    clearForm();
    urlInput?.focus();
  }
</script>

<svelte:head>
  <title>Hister - Add</title>
</svelte:head>

<div class="min-h-0 flex-1 overflow-y-auto">
  <div class="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 md:px-6 md:py-8">
    <section class="min-w-0 space-y-5">
      {#if message}
        <Alert.Root variant={isError ? 'error' : 'success'} class="border-[3px]">
          {#if isError}
            <AlertCircle class="size-4 shrink-0" />
          {:else}
            <CheckCircle class="size-4 shrink-0" />
          {/if}
          <Alert.Description class="font-inter gap-3 text-sm">
            <span>{message}</span>
            {#if addedDocument}
              <div class="flex flex-wrap gap-2">
                <Button
                  href={buildPreviewUrl(
                    addedDocument.url,
                    addedDocument.title || addedDocument.url,
                  )}
                  size="sm"
                  variant="outline"
                  class="border-brutal-border bg-page-bg brutal-press h-8 rounded-none border-[2px] px-3 text-xs font-bold uppercase"
                >
                  <Eye class="size-3.5" />
                  View document
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  class="border-brutal-border bg-page-bg brutal-press h-8 rounded-none border-[2px] px-3 text-xs font-bold uppercase"
                  onclick={addAnother}
                >
                  <Plus class="size-3.5" />
                  Add another
                </Button>
              </div>
            {/if}
          </Alert.Description>
        </Alert.Root>
      {/if}

      <Card.Root>
        <Card.Header class="border-brutal-border border-b-[3px] px-5 py-4">
          <div class="flex w-full flex-col gap-4 md:flex-row md:items-start md:justify-between">
            <div class="flex min-w-0 items-start gap-3">
              <span
                class="bg-hister-coral/10 border-border-brand-muted flex size-10 shrink-0 items-center justify-center border"
              >
                <Database class="text-hister-coral size-5" />
              </span>
              <div class="min-w-0">
                <Card.Title
                  class="font-space text-card-foreground text-xl font-extrabold uppercase"
                >
                  Add document manually
                </Card.Title>
                <Card.Description class="font-inter text-text-brand-secondary text-sm">
                  Save a URL with optional title and content.
                </Card.Description>
              </div>
            </div>
            <div class="flex shrink-0 items-center gap-2">
              <Button
                type="button"
                variant="outline"
                class="border-brutal-border h-10 border-[3px] px-3 text-xs font-bold uppercase"
                onclick={clearForm}
                disabled={submitting || (!url && !title && !text && !message)}
              >
                <RotateCcw class="size-3.5" />
                Clear
              </Button>
              <Button
                type="submit"
                form="add-entry-form"
                disabled={!canSubmit}
                class="bg-hister-coral font-outfit text-primary-foreground brutal-press border-brutal-border hover:bg-hister-coral/90 h-10 shrink-0 gap-2 border-[3px] px-5 text-sm font-extrabold uppercase disabled:opacity-50"
              >
                {#if submitting}
                  <LoaderCircle class="size-4 shrink-0 animate-spin" />
                {:else}
                  <Save class="size-4 shrink-0" />
                {/if}
                <span>{submitting ? 'Saving' : 'Save'}</span>
              </Button>
            </div>
          </div>
        </Card.Header>

        <Card.Content class="p-0">
          <div class="bg-muted-surface border-border-brand space-y-2 border-b px-4 py-3 md:px-5">
            <p class="font-inter text-text-brand-secondary text-sm">
              This form saves only the information you enter. It does not download the page.
            </p>
            <p class="font-inter text-text-brand-secondary text-sm">
              To capture page content automatically as you browse,
              <a
                href="https://hister.org/docs/browser-extension"
                target="_blank"
                rel="noopener noreferrer"
                class="text-hister-indigo font-semibold underline underline-offset-2"
              >
                set up the browser extension<ExternalLink
                  class="ml-1 inline size-3.5 align-baseline"
                  aria-hidden="true"
                />
              </a>.
            </p>
          </div>
          <form id="add-entry-form" onsubmit={handleSubmit} class="divide-border-brand divide-y">
            <div class="grid gap-3 p-4 md:grid-cols-[12rem_minmax(0,1fr)] md:p-5">
              <div class="flex items-start gap-2">
                <span
                  class="border-brutal-border bg-card-surface text-hister-coral flex size-8 shrink-0 items-center justify-center border-[2px]"
                >
                  <Link class="size-4" />
                </span>
                <div class="min-w-0">
                  <Label for="entry-url" class="font-outfit text-text-brand text-sm font-black">
                    URL
                  </Label>
                  <p class="font-fira text-hister-rose mt-1 text-[11px] font-semibold uppercase">
                    Required
                  </p>
                </div>
              </div>
              <div class="min-w-0 space-y-2">
                <Input
                  id="entry-url"
                  type="url"
                  variant="brutal"
                  bind:ref={urlInput}
                  bind:value={url}
                  placeholder="https://example.com/page"
                  required
                  autocomplete="url"
                  aria-describedby="entry-url-help"
                  class="focus-visible:border-hister-coral [font-variant-ligatures:none]"
                />
                <p id="entry-url-help" class="font-inter text-text-brand-muted text-xs">
                  The address to save in your index
                </p>
              </div>
            </div>

            <div class="grid gap-3 p-4 md:grid-cols-[12rem_minmax(0,1fr)] md:p-5">
              <div class="flex items-start gap-2">
                <span
                  class="border-brutal-border bg-card-surface text-hister-coral flex size-8 shrink-0 items-center justify-center border-[2px]"
                >
                  <Type class="size-4" />
                </span>
                <div class="min-w-0">
                  <Label for="entry-title" class="font-outfit text-text-brand text-sm font-black">
                    Title
                  </Label>
                  <p
                    class="font-fira text-text-brand-muted mt-1 text-[11px] font-semibold uppercase"
                  >
                    Optional
                  </p>
                </div>
              </div>
              <div class="min-w-0 space-y-2">
                <Input
                  id="entry-title"
                  type="text"
                  variant="brutal"
                  bind:value={title}
                  placeholder="Page title"
                  autocomplete="off"
                  aria-describedby="entry-title-help"
                  class="font-inter focus-visible:border-hister-coral"
                />
                <p id="entry-title-help" class="font-inter text-text-brand-muted text-xs">
                  Searchable title for the document
                </p>
              </div>
            </div>

            <div class="grid gap-3 p-4 md:grid-cols-[12rem_minmax(0,1fr)] md:p-5">
              <div class="flex items-start gap-2">
                <span
                  class="border-brutal-border bg-card-surface text-hister-coral flex size-8 shrink-0 items-center justify-center border-[2px]"
                >
                  <FileText class="size-4" />
                </span>
                <div class="min-w-0">
                  <Label for="entry-content" class="font-outfit text-text-brand text-sm font-black">
                    Content
                  </Label>
                  <p
                    class="font-fira text-text-brand-muted mt-1 text-[11px] font-semibold uppercase"
                  >
                    Optional
                  </p>
                </div>
              </div>
              <div class="min-w-0 space-y-2">
                <Textarea
                  id="entry-content"
                  bind:value={text}
                  placeholder="Page content"
                  aria-describedby="entry-content-help"
                  class="bg-page-bg border-brutal-border font-inter text-text-brand placeholder:text-text-brand-muted focus-visible:border-hister-coral min-h-64 w-full resize-y rounded-none border-[3px] p-4 text-sm transition-colors outline-none focus-visible:ring-0"
                />
                <div class="flex items-center justify-between gap-3">
                  <p id="entry-content-help" class="font-inter text-text-brand-muted text-xs">
                    Paste the text you want to search later
                  </p>
                  <span class="font-fira text-text-brand-muted shrink-0 text-xs">
                    {contentChars.toLocaleString()} chars
                  </span>
                </div>
              </div>
            </div>
          </form>
        </Card.Content>
      </Card.Root>
    </section>
  </div>
</div>
