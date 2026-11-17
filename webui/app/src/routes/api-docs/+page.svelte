<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import { goto } from '$app/navigation';
  import { apiFetch } from '#lib/api.js';
  import { Badge } from '@hister/components/ui/badge';
  import * as Card from '@hister/components/ui/card';
  import * as Table from '@hister/components/ui/table';
  import { PageHeader } from '@hister/components';

  interface EndpointArg {
    name: string;
    type: string;
    required: boolean;
    description: string;
  }

  interface JSONSchemaField {
    name: string;
    type: string;
    required: boolean;
    description: string;
    fields?: JSONSchemaField[];
  }

  interface APIEndpoint {
    name: string;
    path: string;
    method: string;
    csrf_required: boolean;
    public: boolean;
    requires_auth: boolean;
    mutates: boolean;
    description: string;
    args: EndpointArg[] | null;
    json_schema: JSONSchemaField[] | null;
  }

  let endpoints: APIEndpoint[] = $state([]);
  let loading = $state(true);
  let activeId = $state(page.url.hash.slice(1));
  let isScrolling = $state(false);
  let initialHashHandled = $state(false);
  let scrollContainer: HTMLDivElement | undefined = $state();

  onMount(async () => {
    try {
      const res = await apiFetch('', {
        headers: { Accept: 'application/json' },
      });
      if (res.ok) endpoints = await res.json();
    } finally {
      loading = false;
    }
  });

  $effect(() => {
    if (endpoints.length === 0 || typeof window === 'undefined') return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (isScrolling) return;

        const visible = entries
          .filter((e) => e.isIntersecting)
          .sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0];
        if (visible) {
          activeId = visible.target.id;
        }
      },
      { rootMargin: '-15% 0px -70% 0px', threshold: [0, 0.25, 0.5, 0.75, 1] },
    );

    document.querySelectorAll('[id^="ep-"]').forEach((el) => observer.observe(el));
    return () => observer.disconnect();
  });

  $effect(() => {
    if (loading || endpoints.length === 0 || initialHashHandled || typeof window === 'undefined')
      return;
    initialHashHandled = true;
    const hash = page.url.hash.slice(1);
    if (hash) {
      scrollToEndpoint(hash, true);
    }
  });

  function tocId(ep: APIEndpoint): string {
    return `ep-${ep.method.toLowerCase()}-${ep.path.replace(/[^a-z0-9]+/gi, '-').replace(/^-|-$/g, '')}`;
  }

  function scrollToEndpoint(id: string, replace = false) {
    isScrolling = true;
    activeId = id;

    if (typeof window !== 'undefined') {
      const url = `#${id}`;
      if (replace) {
        void goto(url, { shallow: true, replace: true });
      } else {
        void goto(url, { shallow: true });
      }
    }

    const el = document.getElementById(id);
    const container = scrollContainer;
    if (!el || !container) {
      isScrolling = false;
      return;
    }

    let fallbackTimeout: ReturnType<typeof setTimeout> | undefined;
    const cleanup = () => {
      isScrolling = false;
      clearTimeout(fallbackTimeout);
      container.removeEventListener('scrollend', cleanup);
    };

    fallbackTimeout = setTimeout(cleanup, 1000);
    container.addEventListener('scrollend', cleanup, { once: true });

    el.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }
</script>

<svelte:head>
  <title>Nogon - API</title>
</svelte:head>

<div class="mx-auto flex h-full max-w-[90em] flex-col overflow-hidden md:flex-row">
  {#if !loading}
    <!-- Desktop ToC: floating card on the left -->
    <nav
      class="bg-card-surface my-5 ms-6 hidden h-fit max-h-[calc(100%-2.5rem)] w-56 shrink-0 self-start overflow-y-auto rounded-none border border-black shadow-[4px_4px_0_var(--brutal-shadow)] md:block"
    >
      <div>
        <p
          class="font-outfit text-text-brand bg-card-surface sticky top-0 z-10 border-b border-black px-4 py-3 text-xs font-bold tracking-wider uppercase"
        >
          Contents
        </p>
        <ul class="space-y-0.5 px-4 py-3">
          {#each endpoints as ep}
            <li>
              <a
                href="#{tocId(ep)}"
                class="font-inter flex items-center gap-1.5 rounded-md px-2 py-1.5 text-xs leading-snug transition-colors {activeId ===
                tocId(ep)
                  ? 'bg-page-bg text-text-brand'
                  : 'text-text-brand-secondary hover:bg-page-bg/50 hover:text-text-brand'}"
                onclick={(e) => {
                  e.preventDefault();
                  scrollToEndpoint(tocId(ep));
                }}
              >
                <span
                  class="shrink-0 font-bold {ep.method === 'GET'
                    ? 'text-hister-teal'
                    : 'text-hister-coral'}">{ep.method}</span
                >
                <span class="truncate">{ep.name}</span>
              </a>
            </li>
          {/each}
        </ul>
      </div>
    </nav>
  {/if}

  <div
    class="flex-1 overflow-y-auto px-3 pt-0 pb-4 md:px-6 md:pt-5 md:pb-5"
    bind:this={scrollContainer}
  >
    <div class="mx-auto max-w-[80em]">
      <div class="space-y-1 pt-4 md:mb-5 md:pt-0">
        <PageHeader color="hister-teal" size="sm">API Documentation</PageHeader>
        <p class="font-inter text-text-brand-secondary pb-2 text-xs md:pb-0 md:text-sm">
          Available HTTP endpoints for integrating with Nogon
        </p>
      </div>

      {#if loading}
        <p class="font-inter text-text-brand-muted py-8 text-center text-sm">
          Loading endpoints...
        </p>
      {:else}
        <!-- Mobile ToC: sticky dropdown for jumping to endpoints -->
        <div
          class="bg-card-surface sticky top-0 z-10 -mx-3 mb-3 border-b border-black px-3 py-2 md:hidden"
        >
          <select
            class="font-space border-brutal-border bg-page-bg text-text-brand h-10 w-full cursor-pointer appearance-none border-[3px] px-3 text-xs font-bold tracking-[0.5px] outline-none"
            onchange={(e) => {
              const id = e.currentTarget.value;
              if (!id) return;
              scrollToEndpoint(id);
            }}
          >
            <option value="" disabled>Jump to endpoint...</option>
            {#each endpoints as ep}
              <option value={tocId(ep)}>
                {ep.method}
                {ep.name}
              </option>
            {/each}
          </select>
        </div>

        <div class="min-w-0 space-y-4">
          {#each endpoints as ep}
            <Card.Root
              id={tocId(ep)}
              class="bg-card-surface scroll-mt-16 gap-0 overflow-hidden rounded-none border border-black py-0 shadow-[4px_4px_0_var(--brutal-shadow)] md:scroll-mt-0"
            >
              <Card.Header class="gap-0 px-4 py-3">
                <div class="flex flex-wrap items-center gap-2.5">
                  <Card.Title class="font-outfit text-text-brand text-base font-bold md:text-xl"
                    >{ep.name}</Card.Title
                  >
                  <Badge
                    variant="default"
                    class="border-0 px-2 py-0 text-[11px] leading-5 font-bold {ep.method === 'GET'
                      ? 'bg-hister-teal text-white'
                      : 'bg-hister-coral text-white'}"
                  >
                    {ep.method}
                  </Badge>
                  <code class="font-fira text-text-brand-secondary text-sm">{ep.path}</code>
                  {#if ep.csrf_required}
                    <Badge
                      variant="outline"
                      class="border-hister-amber text-hister-amber border-2 px-1.5 py-0 text-[10px] leading-5 font-semibold"
                    >
                      CSRF
                    </Badge>
                  {/if}
                  {#if ep.public}
                    <Badge
                      variant="outline"
                      class="border-hister-teal text-hister-teal border-2 px-1.5 py-0 text-[10px] leading-5 font-semibold"
                    >
                      Public
                    </Badge>
                  {/if}
                  {#if ep.requires_auth}
                    <Badge
                      variant="outline"
                      class="border-hister-rose text-hister-rose border-2 px-1.5 py-0 text-[10px] leading-5 font-semibold"
                    >
                      Auth
                    </Badge>
                  {/if}
                  {#if ep.mutates}
                    <Badge
                      variant="outline"
                      class="border-hister-coral text-hister-coral border-2 px-1.5 py-0 text-[10px] leading-5 font-semibold"
                    >
                      Write
                    </Badge>
                  {/if}
                </div>
              </Card.Header>

              <Card.Content class="border-t px-4 py-3">
                <p class="font-inter text-text-brand-secondary mb-3 text-sm">{ep.description}</p>

                {#if ep.args && ep.args.length > 0}
                  <h4
                    class="font-outfit text-text-brand-muted mb-2 text-xs font-bold tracking-wider uppercase"
                  >
                    Arguments
                  </h4>
                  <div class="hidden md:block">
                    <Table.Root>
                      <Table.Header>
                        <Table.Row
                          class="bg-muted-surface border-brutal-border hover:bg-muted-surface border-b"
                        >
                          <Table.Head
                            class="font-inter text-text-brand-muted h-auto px-3 py-2 text-xs font-bold"
                            >Name</Table.Head
                          >
                          <Table.Head
                            class="font-inter text-text-brand-muted h-auto px-3 py-2 text-xs font-bold"
                            >Type</Table.Head
                          >
                          <Table.Head
                            class="font-inter text-text-brand-muted h-auto px-3 py-2 text-xs font-bold"
                            >Required</Table.Head
                          >
                          <Table.Head
                            class="font-inter text-text-brand-muted h-auto px-3 py-2 text-xs font-bold"
                            >Description</Table.Head
                          >
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {#each ep.args as arg}
                          <Table.Row class="border-brutal-border/30 border-b">
                            <Table.Cell
                              class="font-fira text-text-brand px-3 py-2 text-sm font-semibold"
                              ><code>{arg.name}</code></Table.Cell
                            >
                            <Table.Cell
                              class="font-fira text-text-brand-secondary px-3 py-2 text-sm"
                              ><code>{arg.type}</code></Table.Cell
                            >
                            <Table.Cell class="px-3 py-2">
                              {#if arg.required}
                                <Badge
                                  variant="default"
                                  class="bg-hister-rose border-0 px-1.5 py-0 text-[10px] text-white"
                                  >required</Badge
                                >
                              {:else}
                                <span class="font-inter text-text-brand-muted text-xs"
                                  >optional</span
                                >
                              {/if}
                            </Table.Cell>
                            <Table.Cell
                              class="font-inter text-text-brand-secondary px-3 py-2 text-sm whitespace-normal"
                              >{arg.description}</Table.Cell
                            >
                          </Table.Row>
                        {/each}
                      </Table.Body>
                    </Table.Root>
                  </div>
                  <div class="space-y-2.5 md:hidden">
                    {#each ep.args as arg}
                      <div class="space-y-0.5">
                        <div class="flex items-center gap-2">
                          <code class="font-fira text-text-brand text-sm font-semibold"
                            >{arg.name}</code
                          >
                          <code class="font-fira text-text-brand-muted text-xs">{arg.type}</code>
                          {#if arg.required}
                            <Badge
                              variant="default"
                              class="bg-hister-rose border-0 px-1.5 py-0 text-[10px] text-white"
                              >required</Badge
                            >
                          {/if}
                        </div>
                        <p class="font-inter text-text-brand-secondary text-xs">
                          {arg.description}
                        </p>
                      </div>
                    {/each}
                  </div>
                {/if}

                {#if ep.json_schema && ep.json_schema.length > 0}
                  {#snippet schemaTableRows(fields: JSONSchemaField[], depth: number)}
                    {#each fields as field}
                      <Table.Row class="border-brutal-border/30 border-b">
                        <Table.Cell
                          class="font-fira text-text-brand px-3 py-2 text-sm font-semibold"
                        >
                          {#if depth > 0}
                            <span class="text-text-brand-muted select-none"
                              >{'  '.repeat(depth)}↳&nbsp;</span
                            >
                          {/if}<code>{field.name}</code>
                        </Table.Cell>
                        <Table.Cell class="font-fira text-text-brand-secondary px-3 py-2 text-sm"
                          ><code>{field.type}</code></Table.Cell
                        >
                        <Table.Cell class="px-3 py-2">
                          {#if field.required}
                            <Badge
                              variant="default"
                              class="bg-hister-rose border-0 px-1.5 py-0 text-[10px] text-white"
                              >required</Badge
                            >
                          {:else}
                            <span class="font-inter text-text-brand-muted text-xs">optional</span>
                          {/if}
                        </Table.Cell>
                        <Table.Cell
                          class="font-inter text-text-brand-secondary px-3 py-2 text-sm whitespace-normal"
                          >{field.description}</Table.Cell
                        >
                      </Table.Row>
                      {#if field.fields && field.fields.length > 0}
                        {@render schemaTableRows(field.fields, depth + 1)}
                      {/if}
                    {/each}
                  {/snippet}

                  {#snippet schemaCards(fields: JSONSchemaField[], depth: number)}
                    {#each fields as field}
                      <div class="space-y-0.5" style="margin-left: {depth * 14}px">
                        <div class="flex items-center gap-2">
                          {#if depth > 0}
                            <span class="text-text-brand-muted text-xs">↳</span>
                          {/if}
                          <code class="font-fira text-text-brand text-sm font-semibold"
                            >{field.name}</code
                          >
                          <code class="font-fira text-text-brand-muted text-xs">{field.type}</code>
                          {#if field.required}
                            <Badge
                              variant="default"
                              class="bg-hister-rose border-0 px-1.5 py-0 text-[10px] text-white"
                              >required</Badge
                            >
                          {/if}
                        </div>
                        <p class="font-inter text-text-brand-secondary text-xs">
                          {field.description}
                        </p>
                      </div>
                      {#if field.fields && field.fields.length > 0}
                        {@render schemaCards(field.fields, depth + 1)}
                      {/if}
                    {/each}
                  {/snippet}

                  <h4
                    class="font-outfit text-text-brand-muted mt-3 mb-2 text-xs font-bold tracking-wider uppercase"
                  >
                    Request Body (JSON)
                  </h4>
                  <div class="hidden md:block">
                    <Table.Root>
                      <Table.Header>
                        <Table.Row
                          class="bg-muted-surface border-brutal-border hover:bg-muted-surface border-b"
                        >
                          <Table.Head
                            class="font-inter text-text-brand-muted h-auto px-3 py-2 text-xs font-bold"
                            >Field</Table.Head
                          >
                          <Table.Head
                            class="font-inter text-text-brand-muted h-auto px-3 py-2 text-xs font-bold"
                            >Type</Table.Head
                          >
                          <Table.Head
                            class="font-inter text-text-brand-muted h-auto px-3 py-2 text-xs font-bold"
                            >Required</Table.Head
                          >
                          <Table.Head
                            class="font-inter text-text-brand-muted h-auto px-3 py-2 text-xs font-bold"
                            >Description</Table.Head
                          >
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {@render schemaTableRows(ep.json_schema, 0)}
                      </Table.Body>
                    </Table.Root>
                  </div>
                  <div class="space-y-2.5 md:hidden">
                    {@render schemaCards(ep.json_schema, 0)}
                  </div>
                {/if}
              </Card.Content>
            </Card.Root>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</div>
