<script lang="ts">
  import Check from '@lucide/svelte/icons/check';
  import FileText from '@lucide/svelte/icons/file-text';
  import Globe2 from '@lucide/svelte/icons/globe-2';
  import Search from '@lucide/svelte/icons/search';

  const results = [
    {
      icon: Globe2,
      title: 'The ultimate way to protect digital privacy',
      source: 'hister.org/posts',
      text: 'Keep useful pages close, searchable, and under your control...',
      accent: 'var(--nogon-indigo)',
    },
    {
      icon: FileText,
      title: 'privacy-notes.md',
      source: '~/documents/notes',
      text: 'A private index makes the things you already found useful again.',
      accent: 'var(--nogon-cyan)',
    },
  ];
</script>

<div class="relative mx-auto w-full max-w-[760px] min-w-0 lg:mx-0">
  <div
    class="border-brutal-border bg-brutal-card overflow-hidden border-[3px] shadow-[10px_10px_0_var(--brutal-shadow)]"
  >
    <div
      class="border-brutal-border flex h-12 items-center justify-between border-b-[3px] bg-[var(--text-primary)] px-4 text-white"
    >
      <div class="flex items-center gap-3">
        <img src="/logo.png" alt="" class="h-6 w-6" />
        <span class="font-space text-xs font-black tracking-[1.8px] uppercase">Nogon</span>
      </div>
      <span class="font-fira text-[9px] tracking-[1px] text-white/70 uppercase"
        >1,924 pages indexed</span
      >
    </div>

    <div class="p-4 sm:p-6">
      <div
        class="border-brutal-border flex items-center gap-3 border-[3px] bg-white px-4 py-3 shadow-[4px_4px_0_var(--nogon-coral)] sm:py-4"
      >
        <Search size={20} class="shrink-0 text-[var(--text-secondary)]" />
        <span
          class="font-inter min-w-0 flex-1 truncate text-sm font-medium text-[var(--text-primary)] sm:text-base"
          >privacy notes I read last month</span
        >
        <span class="bg-nogon-indigo h-5 w-1 shrink-0" aria-hidden="true"></span>
      </div>

      <div class="mt-5 grid gap-4 md:grid-cols-[1.08fr_0.92fr]">
        <div class="flex flex-col gap-3">
          <div class="flex items-center justify-between">
            <span
              class="font-fira text-[9px] font-bold tracking-[1.2px] text-[var(--text-secondary)] uppercase"
              >6 results in your index</span
            >
            <span class="font-fira text-[9px] text-[var(--text-secondary)]">0.04 sec</span>
          </div>

          {#each results as result}
            <article
              class="result-card border-brutal-border border-[2px] bg-white p-4"
              style="--result-accent: {result.accent}"
            >
              <div class="flex items-start gap-3">
                <div
                  class="border-brutal-border mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center border-[2px] bg-[var(--result-accent)] text-white"
                >
                  <svelte:component this={result.icon} size={15} />
                </div>
                <div class="min-w-0">
                  <p
                    class="font-space text-sm leading-tight font-extrabold text-[var(--text-primary)] sm:text-base"
                  >
                    {result.title}
                  </p>
                  <p class="font-fira mt-1 truncate text-[9px] text-[var(--nogon-cyan)]">
                    {result.source}
                  </p>
                </div>
              </div>
              <p class="mt-3 text-xs leading-relaxed text-[var(--text-secondary)]">
                {result.text}
              </p>
            </article>
          {/each}
        </div>

        <aside
          class="border-brutal-border hidden border-[2px] bg-[#f4f0e7] p-4 md:flex md:flex-col"
        >
          <div class="flex items-center justify-between border-b border-black/15 pb-3">
            <span
              class="font-fira text-[9px] font-bold tracking-[1px] text-[var(--text-secondary)] uppercase"
              >Stored preview</span
            >
            <span class="bg-nogon-coral h-2.5 w-2.5"></span>
          </div>
          <p class="font-space mt-4 text-lg leading-tight font-black text-[var(--text-primary)]">
            Search without retracing your steps
          </p>
          <p class="mt-3 text-[11px] leading-[1.65] text-[var(--text-secondary)]">
            Open a clean preview, see where it came from, and get back to the exact idea you
            remembered.
          </p>
          <div class="mt-auto flex flex-col gap-2 pt-5">
            {#each ['Full content saved', 'Source stays visible', 'Searchable again'] as item}
              <span
                class="font-fira flex items-center gap-2 text-[9px] font-semibold text-[var(--text-primary)]"
              >
                <span
                  class="bg-nogon-lime flex h-4 w-4 items-center justify-center text-[var(--text-primary)]"
                  ><Check size={11} strokeWidth={3} /></span
                >
                {item}
              </span>
            {/each}
          </div>
        </aside>
      </div>
    </div>
  </div>
</div>

<style>
  .result-card {
    border-left: 6px solid var(--result-accent);
  }
</style>
