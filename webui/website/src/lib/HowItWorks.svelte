<script lang="ts">
  import Archive from '@lucide/svelte/icons/archive';
  import ArrowDown from '@lucide/svelte/icons/arrow-down';
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import Bot from '@lucide/svelte/icons/bot';
  import Browser from '@lucide/svelte/icons/app-window';
  import FileText from '@lucide/svelte/icons/file-text';
  import FolderSearch from '@lucide/svelte/icons/folder-search';
  import Globe2 from '@lucide/svelte/icons/globe-2';
  import Search from '@lucide/svelte/icons/search';
  import Terminal from '@lucide/svelte/icons/terminal';

  const steps = [
    {
      label: 'Collect',
      description:
        'Save newly visited pages with the browser extension, watch local folders, import your history, or crawl a site.',
      icon: Archive,
      color: 'var(--nogon-coral)',
      items: [
        { icon: Globe2, label: 'Visited pages' },
        { icon: FileText, label: 'Local files' },
      ],
    },
    {
      label: 'Index',
      description:
        'Nogon extracts the parts that matter and indexes their full text on the server you choose.',
      icon: FolderSearch,
      color: 'var(--nogon-amber)',
      items: [
        { icon: FileText, label: 'Full text' },
        { icon: Archive, label: 'Stored context' },
      ],
    },
    {
      label: 'Find',
      description:
        'Search from the web, terminal, command line, or let an AI assistant retrieve it through MCP.',
      icon: Search,
      color: 'var(--nogon-lime)',
      items: [
        { icon: Terminal, label: 'Web and terminal' },
        { icon: Bot, label: 'MCP assistants' },
      ],
    },
  ];
</script>

<section
  id="how-it-works"
  class="border-brutal-border border-b-[3px] bg-[#eeeae1] px-6 py-16 md:px-12 md:py-24"
>
  <div class="mx-auto max-w-[1400px]">
    <header>
      <div>
        <div class="mb-5 flex items-center gap-3">
          <span class="bg-nogon-coral h-3 w-3"></span>
          <p
            class="font-fira text-xs font-bold tracking-[1.8px] text-[var(--text-secondary)] uppercase"
          >
            How Nogon works
          </p>
        </div>
        <h2
          class="font-outfit max-w-[720px] text-4xl leading-[0.95] font-black tracking-[-0.04em] text-[var(--text-primary)] uppercase md:text-6xl lg:text-7xl"
        >
          A private memory without the busywork
        </h2>
      </div>
    </header>

    <div class="mt-12 grid items-stretch gap-0 lg:mt-16 lg:grid-cols-[1fr_auto_1fr_auto_1fr]">
      {#each steps as step, i}
        {@const StepIcon = step.icon}
        <article
          class="step-card border-brutal-border bg-brutal-card relative flex min-h-[410px] flex-col border-[3px] p-6 shadow-[7px_7px_0_var(--brutal-shadow)] md:p-8"
          style="--step-accent: {step.color}"
        >
          <div class="flex items-center gap-5">
            <div
              class="border-brutal-border shadow-brutal flex h-14 w-14 shrink-0 items-center justify-center border-[3px] bg-[var(--step-accent)]"
            >
              <StepIcon size={26} strokeWidth={2.3} />
            </div>
            <p
              class="font-fira text-base font-bold tracking-[2px] text-[var(--text-secondary)] uppercase"
            >
              {step.label}
            </p>
          </div>
          <p class="mt-8 text-sm leading-[1.7] text-[var(--text-secondary)] md:text-base">
            {step.description}
          </p>

          <div class="mt-auto grid grid-cols-2 gap-2 pt-8">
            {#each step.items as item}
              {@const ItemIcon = item.icon}
              <div
                class="border-brutal-border flex flex-col items-center border-[2px] bg-white p-4 text-center"
              >
                <ItemIcon size={32} class="mb-4 text-[var(--text-primary)]" />
                <span
                  class="font-fira block text-[11px] leading-tight font-bold tracking-[0.7px] text-[var(--text-secondary)] uppercase"
                  >{item.label}</span
                >
              </div>
            {/each}
          </div>
        </article>

        {#if i < steps.length - 1}
          <div
            class="flow-arrow relative z-10 flex items-center justify-center py-6 lg:w-14 lg:py-0"
          >
            <div
              class="flex h-10 w-10 items-center justify-center text-[var(--text-primary)] lg:h-12 lg:w-12"
            >
              <ArrowDown size={24} strokeWidth={2.5} class="lg:hidden" />
              <ArrowRight size={24} strokeWidth={2.5} class="hidden lg:block" />
            </div>
          </div>
        {/if}
      {/each}
    </div>

    <div
      class="border-brutal-border mt-12 grid border-[3px] bg-[var(--text-primary)] text-white md:grid-cols-[auto_1fr]"
    >
      <div
        class="md:border-brutal-border flex items-center gap-3 border-b-[3px] border-white/20 bg-[#59598f] px-5 py-4 md:border-r-[3px] md:border-b-0"
      >
        <Browser size={20} />
        <span class="font-space text-xs font-black tracking-[1.4px] uppercase">The simple loop</span
        >
      </div>
      <p class="px-5 py-4 text-sm leading-relaxed text-white/70 md:px-7">
        Browser extensions can index pages as they are visited. File watchers, history imports, and
        crawlers add other sources to the same index.
      </p>
    </div>
  </div>
</section>

<style>
  .step-card {
    transition:
      translate 220ms ease,
      box-shadow 220ms ease;
  }

  .step-card:hover {
    translate: 3px 3px;
    box-shadow: 4px 4px 0 var(--brutal-shadow);
  }

  @media (prefers-reduced-motion: reduce) {
    .step-card {
      transition: none;
    }
  }
</style>
