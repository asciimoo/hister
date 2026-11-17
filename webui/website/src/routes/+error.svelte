<script lang="ts">
  import { page } from '$app/state';
  import BookOpen from '@lucide/svelte/icons/book-open';
  import House from '@lucide/svelte/icons/house';
  import Newspaper from '@lucide/svelte/icons/newspaper';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import SearchX from '@lucide/svelte/icons/search-x';
  import { Button } from '@nogon/components';

  const notFound = $derived(page.status === 404);
  const title = $derived(notFound ? 'Page Not Found' : 'Something Went Wrong');
  const description = $derived(
    notFound
      ? 'The page may have moved, changed its name, or never existed.'
      : 'Hister could not load this page. Try again or continue from one of the links below.',
  );

  const destinations = [
    {
      href: '/',
      label: 'Home',
      description: 'Return to the Hister overview.',
      icon: House,
      color: 'bg-nogon-cyan',
    },
    {
      href: '/docs',
      label: 'Documentation',
      description: 'Find installation and usage guides.',
      icon: BookOpen,
      color: 'bg-nogon-lime',
    },
    {
      href: '/posts',
      label: 'Posts',
      description: 'Read Hister news and practical guides.',
      icon: Newspaper,
      color: 'bg-nogon-amber',
    },
  ];
</script>

<svelte:head>
  <title>{page.status} {title} | Hister</title>
  <meta name="description" content={description} />
  <meta name="robots" content="noindex, nofollow" />
</svelte:head>

<section
  class="border-brutal-border relative isolate overflow-hidden border-b-[3px] px-6 py-16 md:px-12 md:py-24"
>
  <div class="error-grid" aria-hidden="true"></div>
  <div
    class="relative mx-auto grid max-w-6xl items-center gap-12 lg:grid-cols-[0.8fr_1.2fr] lg:gap-20"
  >
    <div class="relative mx-auto w-fit lg:mx-0" aria-hidden="true">
      <div
        class="border-brutal-border bg-nogon-rose shadow-brutal absolute -top-5 -right-5 h-14 w-14 rotate-12 border-[3px]"
      ></div>
      <div
        class="border-brutal-border bg-nogon-amber shadow-brutal absolute -bottom-6 -left-6 h-10 w-20 -rotate-6 border-[3px]"
      ></div>
      <div
        class="bg-brutal-card border-brutal-border relative flex aspect-square w-[min(68vw,320px)] flex-col items-center justify-center border-[4px] shadow-[12px_12px_0_var(--brutal-shadow)]"
      >
        <SearchX size={72} strokeWidth={2.2} class="text-nogon-indigo mb-2" />
        <span class="font-outfit text-8xl leading-none font-black tracking-[-0.06em]">
          {page.status}
        </span>
      </div>
    </div>

    <div>
      <p class="font-fira text-nogon-indigo mb-4 text-sm font-bold tracking-[2px] uppercase">
        Error {page.status}
      </p>
      <h1
        class="font-outfit text-5xl leading-[0.9] font-black tracking-[-0.045em] text-[var(--text-primary)] uppercase md:text-7xl"
      >
        {title}
      </h1>
      <p class="mt-6 max-w-2xl text-lg leading-[1.75] text-[var(--text-secondary)]">
        {description}
      </p>

      <div class="mt-8 flex flex-col gap-4 sm:flex-row">
        <Button
          href="/"
          class="font-space brutal-press-lg border-brutal-border h-auto justify-center rounded-none border-[3px] bg-[#59598f] px-7 py-3.5 text-sm font-bold tracking-[1.2px] text-white uppercase no-underline"
        >
          <House size={18} />
          Go home
        </Button>
        {#if !notFound}
          <Button
            href={page.url.pathname}
            data-sveltekit-reload
            class="bg-brutal-card font-space brutal-press-lg border-brutal-border h-auto justify-center rounded-none border-[3px] px-7 py-3.5 text-sm font-bold tracking-[1.2px] text-[var(--text-primary)] uppercase no-underline"
          >
            <RefreshCw size={18} />
            Try again
          </Button>
        {/if}
      </div>
    </div>
  </div>
</section>

<nav aria-label="Helpful pages" class="bg-brutal-card border-brutal-border border-b-[3px]">
  <ul class="mx-auto grid max-w-6xl list-none p-0 md:grid-cols-3">
    {#each destinations as destination, index}
      {@const Icon = destination.icon}
      <li
        class="border-brutal-border {index < destinations.length - 1
          ? 'border-b-[3px] md:border-r-[3px] md:border-b-0'
          : ''}"
      >
        <a
          href={destination.href}
          class="group flex h-full items-start gap-4 p-6 text-[var(--text-primary)] no-underline transition-colors hover:bg-white focus-visible:bg-white md:p-8"
        >
          <span
            class="{destination.color} border-brutal-border flex h-11 w-11 shrink-0 items-center justify-center border-[2px] transition-transform group-hover:-rotate-3"
          >
            <Icon size={21} strokeWidth={2.4} />
          </span>
          <span>
            <span class="font-outfit block text-lg font-black uppercase">{destination.label}</span>
            <span class="mt-1 block text-sm leading-relaxed text-[var(--text-secondary)]">
              {destination.description}
            </span>
          </span>
        </a>
      </li>
    {/each}
  </ul>
</nav>

<style>
  section {
    background:
      radial-gradient(
        circle at 15% 20%,
        color-mix(in srgb, var(--nogon-coral) 18%, transparent),
        transparent 32%
      ),
      radial-gradient(
        circle at 86% 72%,
        color-mix(in srgb, var(--nogon-indigo) 15%, transparent),
        transparent 35%
      ),
      var(--brutal-bg);
  }

  .error-grid {
    position: absolute;
    inset: 0;
    opacity: 0.28;
    background-image:
      linear-gradient(
        color-mix(in srgb, var(--brutal-border) 10%, transparent) 1px,
        transparent 1px
      ),
      linear-gradient(
        90deg,
        color-mix(in srgb, var(--brutal-border) 10%, transparent) 1px,
        transparent 1px
      );
    background-size: 32px 32px;
    mask-image: linear-gradient(to bottom, black, transparent 90%);
  }

  @media (prefers-reduced-motion: reduce) {
    a,
    span {
      transition: none;
    }
  }
</style>
