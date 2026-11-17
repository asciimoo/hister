<script lang="ts">
  import { onMount } from 'svelte';
  import { PageHeader } from '@nogon/components';
  import { fetchExtractors, type ExtractorInfo } from '#lib/api.js';

  let extractors = $state<ExtractorInfo[]>([]);
  let error = $state<string | null>(null);

  onMount(async () => {
    try {
      extractors = await fetchExtractors();
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load extractors';
    }
  });

  function formatOptionValue(value: unknown): string {
    if (value === null || value === undefined) return 'null';
    if (typeof value === 'boolean') return value ? 'true' : 'false';
    if (Array.isArray(value)) return value.join(', ');
    return String(value);
  }
</script>

<svelte:head>
  <title>Nogon - Extractors</title>
</svelte:head>

{#snippet capabilityBadge(enabled: boolean, label: string, colorClass: string)}
  {#if enabled}
    <span
      class="{colorClass} font-space rounded px-2 py-0.5 text-[11px] font-semibold tracking-wider uppercase"
      >{label}</span
    >
  {/if}
{/snippet}

<div class="flex-1 overflow-y-auto px-4 py-6 md:px-12 md:py-10">
  <PageHeader color="nogon-green" class="mx-auto mb-8 max-w-3xl">Extractors</PageHeader>

  <div class="mx-auto max-w-3xl">
    {#if error}
      <p class="text-destructive font-inter">{error}</p>
    {:else if extractors.length === 0}
      <p class="text-text-brand-secondary font-inter">Loading...</p>
    {:else}
      <p class="font-inter text-text-brand-secondary mb-8">
        Extractors process web pages before they are added to the index. Each extractor targets a
        specific type of content and can be enabled or disabled independently.
      </p>
      <div class="flex flex-col gap-4">
        {#each extractors as ext (ext.name)}
          <div class="border-brutal-border bg-brutal-bg rounded-brutal border-[3px] px-6 py-5">
            <div class="mb-2 flex items-center gap-3">
              <span class="font-outfit text-text-brand text-lg font-extrabold">{ext.name}</span>
              {#if ext.enabled}
                <span
                  class="bg-nogon-green/20 text-nogon-green font-space rounded px-2 py-0.5 text-[11px] font-semibold tracking-wider uppercase"
                  >Enabled</span
                >
              {:else}
                <span
                  class="bg-text-brand-muted/10 text-text-brand-muted font-space rounded px-2 py-0.5 text-[11px] font-semibold tracking-wider uppercase"
                  >Disabled</span
                >
              {/if}
              {@render capabilityBadge(
                ext.capabilities.enrich,
                'Enricher',
                'bg-nogon-teal/15 text-nogon-teal',
              )}
              {@render capabilityBadge(
                ext.capabilities.extract,
                'Content',
                'bg-nogon-amber/15 text-nogon-amber',
              )}
              {@render capabilityBadge(
                ext.capabilities.preview,
                'Preview',
                'bg-nogon-indigo/15 text-nogon-indigo',
              )}
            </div>
            <p class="font-inter text-text-brand-secondary text-sm leading-relaxed">
              {ext.description}
            </p>
            {#if ext.options && Object.keys(ext.options).length > 0}
              <div class="border-brutal-border mt-4 border-t pt-4">
                <p
                  class="font-space text-text-brand-muted mb-2 text-[11px] font-semibold tracking-wider uppercase"
                >
                  Configuration
                </p>
                <table class="w-full text-sm">
                  <tbody>
                    {#each Object.entries(ext.options) as [key, value] (key)}
                      <tr class="border-border-brand-muted border-b last:border-0">
                        <td class="font-space text-text-brand py-1.5 pr-4 font-medium">{key}</td>
                        <td class="font-inter text-text-brand-secondary py-1.5">
                          <code class="bg-brutal-bg rounded px-1 py-0.5 text-xs"
                            >{formatOptionValue(value)}</code
                          >
                        </td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>
