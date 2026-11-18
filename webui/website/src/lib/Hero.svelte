<script lang="ts">
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import Code2 from '@lucide/svelte/icons/code-2';
  import ExternalLink from '@lucide/svelte/icons/external-link';
  import Lock from '@lucide/svelte/icons/lock';
  import Server from '@lucide/svelte/icons/server';
  import { Button } from '@nogon/components';
  import { onMount } from 'svelte';
  import HeroSearchDemo from '#lib/HeroSearchDemo.svelte';

  const chromeExtensionURL =
    'https://chromewebstore.google.com/detail/nogon/cciilamhchpmbdnniabclekddabkifhb';
  const firefoxExtensionURL = 'https://addons.mozilla.org/en-US/firefox/addon/nogon/';

  let extensionDownloadURL = chromeExtensionURL;

  onMount(() => {
    if (/(Firefox|FxiOS)\//i.test(navigator.userAgent)) {
      extensionDownloadURL = firefoxExtensionURL;
    }
  });

  const trustItems = [
    {
      icon: Code2,
      title: 'Free software',
      copy: 'AGPLv3',
      iconClass: 'bg-nogon-cyan text-white',
    },
    {
      icon: Server,
      title: 'Self hosted',
      copy: 'Run it on your own machine or server',
      iconClass: 'bg-nogon-amber text-[var(--text-primary)]',
    },
    {
      icon: Lock,
      title: 'Privacy focused',
      copy: 'No telemetry, no external requests ',
      iconClass: 'bg-nogon-lime text-[var(--text-primary)]',
    },
  ];
</script>

<section class="hero-shell border-brutal-border relative isolate overflow-hidden border-b-[3px]">
  <div class="hero-grid" aria-hidden="true"></div>
  <div class="hero-shape hero-shape-one" aria-hidden="true"></div>
  <div class="hero-shape hero-shape-two" aria-hidden="true"></div>

  <div
    class="relative z-10 mx-auto grid w-full max-w-[1500px] items-center gap-12 px-6 py-14 md:px-12 md:py-20 lg:grid-cols-[0.88fr_1.12fr] lg:gap-16 lg:py-48"
  >
    <div class="hero-copy flex min-w-0 flex-col items-start">
      <h1
        class="font-outfit max-w-[760px] text-[clamp(3.2rem,8vw,7.6rem)] leading-[0.82] font-black tracking-[-0.055em] text-[var(--text-primary)] uppercase"
      >
        Your Own<br />
        <span class="hero-title-accent">Search Engine</span>
      </h1>

      <p class="mt-8 max-w-[650px] text-lg leading-[1.65] text-[var(--text-secondary)] md:text-xl">
        Nogon turns the pages you visit and the files you keep into a private, full content search
        index that you control.
      </p>
      <p class="mt-3 text-sm text-[var(--text-secondary)]">Formerly known as Hister.</p>

      <div class="mt-9 flex w-full flex-col gap-4 sm:w-auto sm:flex-row">
        <Button
          href="/docs/quickstart"
          class="font-space brutal-press-lg border-brutal-border h-auto justify-center rounded-none border-[3px] bg-[#59598f] px-8 py-4 text-[15px] font-bold tracking-[1.3px] text-white uppercase no-underline"
        >
          Get started
          <ArrowRight size={18} />
        </Button>
        <Button
          href={extensionDownloadURL}
          target="_blank"
          rel="noopener noreferrer"
          class="bg-brutal-card font-space brutal-press-lg border-brutal-border h-auto justify-center rounded-none border-[3px] px-8 py-4 text-[15px] font-bold tracking-[1.3px] text-[var(--text-primary)] uppercase no-underline hover:bg-[var(--text-primary)] hover:text-white"
        >
          Download extension
          <ArrowRight size={18} />
        </Button>
      </div>

      <a
        href="https://github.com/asciimoo/nogon"
        target="_blank"
        rel="noopener noreferrer"
        class="font-fira mt-6 inline-flex items-center gap-2 text-xs font-semibold text-[var(--text-secondary)] underline decoration-2 underline-offset-4 transition-colors hover:text-[var(--text-primary)]"
      >
        <Code2 size={15} />
        Free software on GitHub
        <ExternalLink size={13} />
      </a>
    </div>

    <HeroSearchDemo />
  </div>
</section>

<section class="border-brutal-border bg-brutal-card border-b-[3px]">
  <div class="mx-auto grid w-full max-w-[1500px] md:grid-cols-3">
    {#each trustItems as item, i}
      <article
        class="flex items-center gap-4 px-6 py-5 {i < 2
          ? 'border-brutal-border border-b-[3px] md:border-r-[3px] md:border-b-0'
          : ''} md:px-9"
      >
        <div
          class="border-brutal-border flex h-10 w-10 shrink-0 items-center justify-center border-[2px] {item.iconClass}"
        >
          <svelte:component this={item.icon} size={19} strokeWidth={2.4} />
        </div>
        <div>
          <h2
            class="font-space text-sm font-black tracking-[1px] text-[var(--text-primary)] uppercase"
          >
            {item.title}
          </h2>
          <p class="mt-1 text-xs text-[var(--text-secondary)]">{item.copy}</p>
        </div>
      </article>
    {/each}
  </div>
</section>

<style>
  .hero-shell {
    background:
      radial-gradient(
        circle at 18% 12%,
        color-mix(in srgb, var(--nogon-amber) 20%, transparent),
        transparent 30%
      ),
      radial-gradient(
        circle at 84% 80%,
        color-mix(in srgb, var(--nogon-indigo) 17%, transparent),
        transparent 34%
      ),
      var(--brutal-bg);
  }

  .hero-grid {
    position: absolute;
    inset: 0;
    opacity: 0.34;
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
    mask-image: linear-gradient(to bottom, black, transparent 86%);
  }

  .hero-shape {
    position: absolute;
    border: 3px solid var(--brutal-border);
    box-shadow: 5px 5px 0 var(--brutal-shadow);
  }

  .hero-shape-one {
    top: 7%;
    right: 3%;
    width: 34px;
    height: 34px;
    background: var(--nogon-cyan);
    rotate: 12deg;
  }

  .hero-shape-two {
    bottom: 16%;
    left: 2%;
    width: 25px;
    height: 52px;
    background: var(--nogon-rose);
    rotate: -9deg;
  }

  .hero-title-accent {
    color: var(--nogon-indigo);
    text-shadow: 4px 4px 0 color-mix(in srgb, var(--nogon-coral) 42%, transparent);
  }

  @media (max-width: 640px) {
    .hero-shape {
      display: none;
    }
  }
</style>
