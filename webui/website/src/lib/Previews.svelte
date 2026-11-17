<script lang="ts">
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import Eye from '@lucide/svelte/icons/eye';
  import Filter from '@lucide/svelte/icons/list-filter';
  import Search from '@lucide/svelte/icons/search';
  import { BrowserFrame } from '@nogon/components';

  import screenshot from '#lib/assets/screenshot.png';

  const proofPoints = [
    {
      icon: Search,
      title: 'Search the actual content',
      text: 'Look beyond titles and URLs to the words inside every indexed document.',
      color: 'var(--nogon-cyan)',
    },
    {
      icon: Filter,
      title: 'Narrow with precision',
      text: 'Use fields, phrases, wildcards, negation, priorities, and your own aliases.',
      color: 'var(--nogon-amber)',
    },
    {
      icon: Eye,
      title: 'Read it in context',
      text: 'Open a clean stored preview beside the results without losing your search.',
      color: 'var(--nogon-lime)',
    },
  ];
</script>

<section id="product" class="border-brutal-border bg-brutal-bg overflow-hidden border-b-[3px]">
  <div class="mx-auto max-w-[1500px] px-6 py-16 md:px-12 md:py-24">
    <div class="grid items-center gap-12 lg:grid-cols-[0.72fr_1.28fr] lg:gap-16">
      <div class="flex flex-col items-start">
        <h2
          class="font-outfit max-w-[620px] text-4xl leading-[0.95] font-black tracking-[-0.04em] text-[var(--text-primary)] uppercase md:text-6xl"
        >
          Search the content you chose to index
        </h2>
        <p
          class="mt-6 max-w-[560px] text-base leading-[1.75] text-[var(--text-secondary)] md:text-lg"
        >
          Look beyond bookmarks and filenames. Hister indexes the full content of the pages and
          files you choose, then keeps that searchable knowledge on a server you control.
        </p>

        <div class="mt-9 flex w-full flex-col gap-3">
          {#each proofPoints as point}
            <article
              class="proof-point border-brutal-border bg-brutal-card grid grid-cols-[auto_1fr] gap-4 border-[2px] p-4"
              style="--proof-color: {point.color}"
            >
              <div
                class="border-brutal-border flex h-10 w-10 items-center justify-center border-[2px] bg-[var(--proof-color)]"
              >
                <svelte:component this={point.icon} size={18} />
              </div>
              <div>
                <h3 class="font-space text-sm font-black text-[var(--text-primary)] uppercase">
                  {point.title}
                </h3>
                <p class="mt-1 text-xs leading-relaxed text-[var(--text-secondary)]">
                  {point.text}
                </p>
              </div>
            </article>
          {/each}
        </div>

        <a
          href="/docs/query-language"
          class="font-space decoration-nogon-coral mt-8 inline-flex items-center gap-2 text-xs font-bold tracking-[1.2px] text-[var(--text-primary)] uppercase underline decoration-[3px] underline-offset-4 hover:no-underline"
        >
          Explore the query language
          <ArrowRight size={15} />
        </a>
      </div>

      <div class="relative min-w-0 lg:py-8">
        <div
          class="border-brutal-border bg-nogon-amber shadow-brutal absolute -top-2 -right-3 h-28 w-28 border-[3px] sm:-right-7 lg:top-0"
          aria-hidden="true"
        ></div>
        <div
          class="border-brutal-border bg-nogon-rose shadow-brutal absolute -bottom-4 -left-3 h-20 w-20 border-[3px] sm:-left-8"
          aria-hidden="true"
        ></div>
        <div class="relative z-10">
          <BrowserFrame>
            <img
              src={screenshot}
              alt="Hister search results with a stored document preview open"
              width="1052"
              height="710"
              loading="lazy"
              decoding="async"
              class="block h-auto w-full"
            />
          </BrowserFrame>
        </div>
      </div>
    </div>
  </div>
</section>

<style>
  .proof-point {
    border-left: 7px solid var(--proof-color);
    transition:
      translate 180ms ease,
      box-shadow 180ms ease;
  }

  .proof-point:hover {
    translate: 3px 0;
    box-shadow: -3px 0 0 var(--proof-color);
  }

  @media (prefers-reduced-motion: reduce) {
    .proof-point {
      transition: none;
    }
  }
</style>
