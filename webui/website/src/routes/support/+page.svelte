<script lang="ts">
  import Heart from '@lucide/svelte/icons/heart';
  import GitPullRequest from '@lucide/svelte/icons/git-pull-request';
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import Puzzle from '@lucide/svelte/icons/puzzle';
  import Database from '@lucide/svelte/icons/database';
  import Globe from '@lucide/svelte/icons/globe';
  import Network from '@lucide/svelte/icons/network';
  import Bitcoin from '@lucide/svelte/icons/bitcoin';
  import Coins from '@lucide/svelte/icons/coins';
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import { Button } from '@nogon/components';
  import Seo from '#lib/Seo.svelte';

  const cryptoWallets = [
    {
      name: 'Bitcoin',
      ticker: 'BTC',
      address: '1JwT5gEFYP358GvcdNCKbwgTKvwFujnHSF',
      href: 'bitcoin:1JwT5gEFYP358GvcdNCKbwgTKvwFujnHSF',
      icon: Bitcoin,
      color: 'bg-nogon-amber',
    },
    {
      name: 'Ethereum',
      ticker: 'ETH',
      address: '0x4Eb476D91c92b744600F282F7B727e8b46eDe8Bb',
      href: 'ethereum:0x4Eb476D91c92b744600F282F7B727e8b46eDe8Bb',
      icon: Coins,
      color: 'bg-nogon-indigo',
    },
  ];

  let copiedAddress = $state<string | null>(null);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;

  async function copyWalletAddress(address: string) {
    try {
      await navigator.clipboard.writeText(address);
      copiedAddress = address;
      clearTimeout(copyTimer);
      copyTimer = setTimeout(() => {
        copiedAddress = null;
      }, 1500);
    } catch {
      copiedAddress = null;
    }
  }

  const futurePlans = [
    {
      icon: Network,
      title: 'Federation and Distributed Search',
      desc: 'Connect trusted Nogon instances so they can exchange selected public results, datasets, and index metadata while administrators remain in control of what they share and import.',
      href: null,
      linkLabel: null,
      color: 'bg-nogon-amber',
    },
    {
      icon: Puzzle,
      title: 'Content-Specific Extractors',
      desc: 'Add extractors for more content types to improve integration and index quality so Nogon understands the web deeply, not just superficially.',
      href: '/docs/extractors',
      linkLabel: 'Learn about extractors',
      color: 'bg-nogon-indigo',
    },
    {
      icon: Database,
      title: 'Thematic Pre-Indexed Databases',
      desc: 'Create infrastructure for importable, pre-indexed databases organized by topic, letting users quickly expand their local index with curated, relevant content.',
      href: '/datasets',
      linkLabel: 'Explore datasets',
      color: 'bg-nogon-teal',
    },
    {
      icon: Globe,
      title: 'Public Dataset Imports',
      desc: 'Support importing data from public datasets such as the Open Search Foundation and the Internet Archive, giving Nogon access to the breadth of human knowledge.',
      href: null,
      linkLabel: null,
      color: 'bg-nogon-coral',
    },
  ];
</script>

<Seo
  title="Support Nogon. Help Build the Future of Search"
  description="Support Nogon's mission to eliminate dependence on untrusted search services by contributing code or sponsoring ongoing development."
  path="/support"
/>

<section class="border-brutal-border border-t-[3px] bg-[var(--text-primary)] px-6 py-10">
  <div class="mx-auto flex max-w-5xl flex-col items-center gap-8">
    <Heart size={256} class="fill-[var(--nogon-rose)] text-[var(--nogon-rose)]" />
    <h1
      class="font-outfit text-center text-4xl leading-[0.95] font-black tracking-[-1.5px] text-white uppercase md:text-6xl"
    >
      Join Us in Redefining Search
    </h1>
    <p class="font-inter max-w-[50em] text-center text-lg leading-relaxed text-white/65">
      Whether through code, suggestions or sponsorship, every contribution helps make private,
      powerful search available to everyone.
    </p>
    <div class="grid w-full grid-cols-1 gap-6 md:grid-cols-2">
      <!-- Sponsor -->
      <div
        class="border-brutal-border brutal-press-card flex flex-col gap-6 border-[3px] bg-white p-8 md:p-10"
      >
        <div class="border-brutal-border bg-nogon-rose inline-flex w-fit border-[3px] p-3">
          <Heart size={28} class="fill-white text-white" />
        </div>
        <div class="flex flex-col gap-3">
          <h3
            class="font-space text-2xl font-black tracking-[-0.5px] text-[var(--text-primary)] uppercase"
          >
            Sponsor Development
          </h3>
          <p class="font-inter text-base leading-[1.7] text-[var(--text-secondary)]">
            Financial support directly funds to work on Nogon. Faster bug fixes, new features, and a
            more polished experience for everyone.
          </p>
        </div>
        <div class="mt-auto">
          <Button
            href="https://github.com/sponsors/asciimoo"
            target="_blank"
            rel="noopener noreferrer"
            class="bg-nogon-rose font-space border-brutal-border brutal-press h-auto rounded-none border-[3px] px-7 py-3.5 text-[14px] font-bold tracking-[1.5px] text-white uppercase no-underline"
          >
            <Heart size={16} class="shrink-0 fill-white text-white" />
            Donate
            <ArrowRight size={16} class="shrink-0" />
          </Button>
        </div>
      </div>

      <!-- Contribute -->
      <div
        class="border-brutal-border brutal-press-card flex flex-col gap-6 border-[3px] bg-white p-8 md:p-10"
      >
        <div class="border-brutal-border bg-nogon-indigo inline-flex w-fit border-[3px] p-3">
          <GitPullRequest size={28} class="text-white" />
        </div>
        <div class="flex flex-col gap-3">
          <h3
            class="font-space text-2xl font-black tracking-[-0.5px] text-[var(--text-primary)] uppercase"
          >
            Contribute Code
          </h3>
          <p class="font-inter text-base leading-[1.7] text-[var(--text-secondary)]">
            Nogon is free, open-source software. Whether it's a bug report, a pull request, or
            improving documentation, every contribution moves the project forward.
          </p>
        </div>
        <div class="mt-auto">
          <Button
            href="https://github.com/asciimoo/hister"
            target="_blank"
            rel="noopener noreferrer"
            class="bg-nogon-indigo font-space border-brutal-border brutal-press h-auto rounded-none border-[3px] px-7 py-3.5 text-[14px] font-bold tracking-[1.5px] text-white uppercase no-underline"
          >
            <GitPullRequest size={16} class="shrink-0" />
            View on GitHub
            <ArrowRight size={16} class="shrink-0" />
          </Button>
        </div>
      </div>

      <!-- Cryptocurrency -->
      <div
        class="border-brutal-border brutal-press-card flex flex-col gap-6 border-[3px] bg-white p-8 md:col-span-2 md:p-10"
      >
        <div class="flex flex-col gap-3">
          <h3
            class="font-space text-2xl font-black tracking-[-0.5px] text-[var(--text-primary)] uppercase"
          >
            Donate with Cryptocurrency
          </h3>
          <p class="font-inter text-base leading-[1.7] text-[var(--text-secondary)]">
            Send BTC or ETH directly to support Nogon development.
          </p>
        </div>

        <div class="grid grid-cols-1 gap-5 md:grid-cols-2">
          {#each cryptoWallets as wallet}
            {@const WalletIcon = wallet.icon}
            <div class="border-brutal-border flex min-w-0 flex-col gap-4 border-[3px] p-5">
              <div class="flex items-center gap-3">
                <div class="border-brutal-border {wallet.color} border-[3px] p-2.5">
                  <WalletIcon size={24} class="text-white" />
                </div>
                <div>
                  <h4 class="font-space text-lg font-black text-[var(--text-primary)] uppercase">
                    {wallet.name}
                  </h4>
                  <p
                    class="font-space text-xs font-bold tracking-[1.5px] text-[var(--text-secondary)] uppercase"
                  >
                    {wallet.ticker}
                  </p>
                </div>
              </div>

              <a
                href={wallet.href}
                class="font-fira text-sm leading-relaxed font-semibold break-all text-[var(--text-primary)] underline hover:no-underline"
                title="Open {wallet.ticker} wallet"
              >
                {wallet.address}
              </a>

              <button
                type="button"
                class="font-space border-brutal-border brutal-press inline-flex w-fit cursor-pointer items-center gap-2 border-[3px] bg-[var(--text-primary)] px-4 py-2 text-xs font-bold tracking-[1px] text-white uppercase"
                aria-label="Copy {wallet.ticker} wallet address"
                onclick={() => copyWalletAddress(wallet.address)}
              >
                {#if copiedAddress === wallet.address}
                  <Check size={15} class="shrink-0" />
                  <span aria-live="polite">Copied</span>
                {:else}
                  <Copy size={15} class="shrink-0" />
                  Copy address
                {/if}
              </button>
            </div>
          {/each}
        </div>
      </div>
    </div>
  </div>
</section>

<!-- Mission -->
<section class="bg-brutal-bg border-brutal-border w-full border-b-[3px]">
  <div class="mx-auto grid max-w-5xl grid-cols-1 gap-0 md:grid-cols-2">
    <article class="border-brutal-border flex flex-col gap-6 p-10 md:border-r-[3px] md:p-14">
      <div
        class="bg-nogon-rose border-brutal-border inline-flex w-fit items-center gap-2 border-[3px] px-3.5 py-1.5"
      >
        <span class="font-space text-[11px] font-bold tracking-[2px] text-white uppercase"
          >The Mission</span
        >
      </div>
      <h2
        class="font-outfit text-4xl leading-[1] font-black tracking-[-1.5px] text-[var(--text-primary)] uppercase md:text-5xl"
      >
        Break Free From Untrusted Search
      </h2>
    </article>

    <article class="flex flex-col justify-center gap-5 p-10 md:p-14">
      <p class="font-inter text-lg leading-[1.8] text-[var(--text-secondary)]">
        Our goal is to <strong class="text-[var(--text-primary)]"
          >reduce and ultimately eliminate dependence on untrusted search services</strong
        > by providing the best overall private search experience that comes with batteries included and
        supports privacy at every level.
      </p>
      <p class="font-inter text-lg leading-[1.8] text-[var(--text-secondary)]">
        This is a fresh, independent approach to search. To make it a reality, we need your support.
      </p>
    </article>
  </div>
</section>

<!-- Future Plans -->
<section class="bg-nogon-rose w-full px-6 py-14 text-white md:px-12 md:py-20">
  <div class="mx-auto max-w-5xl">
    <div class="mb-12 flex flex-col items-center gap-4 text-center">
      <h2
        class="font-outfit text-4xl leading-[0.95] tracking-[-1.5px] text-[var(--text-primary)] text-white uppercase md:text-6xl"
      >
        Future Plans
      </h2>
      <p class="font-inter max-w-[540px] text-lg leading-relaxed text-white">
        Where your support takes Nogon next.<br /> These are the major pillars of the roadmap.
      </p>
    </div>

    <div class="flex flex-col gap-6">
      {#each futurePlans as plan, i}
        {@const PlanIcon = plan.icon}
        <div
          class="border-brutal-border grid grid-cols-1 overflow-hidden border-[3px] md:grid-cols-[auto_1fr]"
        >
          <div class="{plan.color} flex items-center justify-center p-8 md:w-[120px]">
            <PlanIcon size={40} class="text-white" />
          </div>
          <div class="bg-brutal-card flex flex-col justify-center gap-3 p-8 md:p-10">
            <h3
              class="font-space text-xl font-black tracking-[-0.5px] text-[var(--text-primary)] uppercase md:text-2xl"
            >
              {plan.title}
            </h3>
            <p class="font-inter text-base leading-[1.7] text-[var(--text-secondary)]">
              {plan.desc}
            </p>
            {#if plan.href}
              <a
                href={plan.href}
                class="font-space mt-1 inline-flex items-center gap-1.5 text-[13px] font-semibold tracking-[1px] text-[var(--text-primary)] uppercase underline hover:no-underline"
              >
                {plan.linkLabel}
                <ArrowRight size={14} class="shrink-0" />
              </a>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  </div>
</section>
