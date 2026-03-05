<script lang="ts">
  import Chrome from '@lucide/svelte/icons/chrome';
  import Globe from '@lucide/svelte/icons/globe';

  let { data } = $props();

  const categoryColors: Record<string, string> = {
    'Getting Started': 'bg-hister-indigo',
    Reference: 'bg-hister-teal',
    Deployment: 'bg-hister-coral',
  };
</script>

<svelte:head>
  <title>Documentation | Hister</title>
</svelte:head>

<section class="max-w-4xl mx-auto px-6 md:px-12 py-12">
  <h1
    class="font-space text-4xl md:text-5xl font-black tracking-[-1px] uppercase text-[var(--text-primary)] mb-10"
  >
    Documentation
  </h1>

  {#each data.categories as category}
    <div class="mb-8">
      <div class="flex items-center gap-2 mb-3">
        <div class="w-2.5 h-2.5 {categoryColors[category.name] ?? 'bg-brutal-border'}"></div>
        <span
          class="font-space text-xs font-bold tracking-[2px] uppercase text-[var(--text-secondary)]"
        >
          {category.name}
        </span>
      </div>

      <ul class="flex flex-col gap-3 list-none m-0 p-0">
        {#each category.docs as doc}
          <li>
            <a
              href="/docs/{doc.slug}/"
              class="block p-5 bg-brutal-card border-[3px] border-brutal-border shadow-[6px_6px_0_var(--brutal-shadow)] no-underline hover:shadow-[3px_3px_0_var(--brutal-shadow)] hover:translate-x-[3px] hover:translate-y-[3px] transition-all"
            >
              <h2
                class="font-space text-lg font-extrabold tracking-[0.5px] text-[var(--text-primary)]"
              >
                {doc.title}
              </h2>
            </a>
          </li>
        {/each}
      </ul>
    </div>
  {/each}

  <div class="mb-8">
    <div class="flex items-center gap-2 mb-3">
      <div class="w-2.5 h-2.5 bg-hister-indigo"></div>
      <span
        class="font-space text-xs font-bold tracking-[2px] uppercase text-[var(--text-secondary)]"
      >
        Browser Extensions
      </span>
    </div>

    <ul class="flex flex-col gap-3 list-none m-0 p-0">
      <li>
        <a
          href="https://chromewebstore.google.com/detail/hister/cciilamhchpmbdnniabclekddabkifhb"
          target="_blank"
          rel="noopener noreferrer"
          class="flex items-center gap-4 p-5 bg-brutal-card border-[3px] border-brutal-border shadow-[6px_6px_0_var(--brutal-shadow)] no-underline hover:shadow-[3px_3px_0_var(--brutal-shadow)] hover:translate-x-[3px] hover:translate-y-[3px] transition-all"
        >
          <Chrome size={20} class="text-[var(--text-secondary)] shrink-0" />
          <h2 class="font-space text-lg font-extrabold tracking-[0.5px] text-[var(--text-primary)]">
            Chrome Extension
          </h2>
        </a>
      </li>
      <li>
        <a
          href="https://addons.mozilla.org/en-US/firefox/addon/hister/"
          target="_blank"
          rel="noopener noreferrer"
          class="flex items-center gap-4 p-5 bg-brutal-card border-[3px] border-brutal-border shadow-[6px_6px_0_var(--brutal-shadow)] no-underline hover:shadow-[3px_3px_0_var(--brutal-shadow)] hover:translate-x-[3px] hover:translate-y-[3px] transition-all"
        >
          <Globe size={20} class="text-[var(--text-secondary)] shrink-0" />
          <h2 class="font-space text-lg font-extrabold tracking-[0.5px] text-[var(--text-primary)]">
            Firefox Add-on
          </h2>
        </a>
      </li>
    </ul>
  </div>
</section>
