<script lang="ts">
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import Check from '@lucide/svelte/icons/check';
  import CloudOff from '@lucide/svelte/icons/cloud-off';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import LockKeyhole from '@lucide/svelte/icons/lock-keyhole';
  import Server from '@lucide/svelte/icons/server';
  import ShieldCheck from '@lucide/svelte/icons/shield-check';

  const principles = [
    {
      icon: EyeOff,
      title: 'No telemetry',
      text: 'The server does not phone home or report what you search.',
      color: 'var(--nogon-coral)',
    },
    {
      icon: CloudOff,
      title: 'No mandatory cloud',
      text: 'A complete personal setup can run on one local machine.',
      color: 'var(--nogon-amber)',
    },
    {
      icon: LockKeyhole,
      title: 'Your chosen server',
      text: 'Clients send indexed content only to the Nogon server you configure.',
      color: 'var(--nogon-lime)',
    },
    {
      icon: ShieldCheck,
      title: 'Auditable software',
      text: 'The source is public and licensed as free software under AGPLv3.',
      color: 'var(--nogon-cyan)',
    },
  ];
</script>

<section
  id="privacy"
  class="border-brutal-border border-b-[3px] bg-[var(--text-primary)] text-white"
>
  <div class="mx-auto max-w-[1500px] px-6 py-16 md:px-12 md:py-24">
    <div class="grid gap-12 lg:grid-cols-[0.9fr_1.1fr] lg:gap-20">
      <div class="flex min-w-0 flex-col items-start">
        <div
          class="mb-7 inline-flex items-center gap-2 border-[2px] border-white/40 bg-[#924653] px-3 py-1.5 shadow-[4px_4px_0_rgba(255,255,255,0.18)]"
        >
          <ShieldCheck size={15} />
          <span class="font-fira text-[10px] font-bold tracking-[1.7px] uppercase">Why Nogon</span>
        </div>
        <h2
          class="font-outfit max-w-[720px] text-4xl leading-[0.9] font-black tracking-[-0.045em] uppercase md:text-7xl"
        >
          Preserve Knowledge.<br />
          <span class="text-nogon-amber">Keep Control.</span>
        </h2>
        <p class="mt-7 max-w-[620px] text-lg leading-[1.8] text-white/65">
          The index, stored page content, and rules remain on the Nogon server you configure. The
          server has no telemetry and does not require a cloud service.
        </p>

        <a
          href="/docs/intro#privacy"
          class="font-space border-nogon-coral hover:text-nogon-coral mt-9 inline-flex items-center gap-2 border-b-[3px] pb-1 text-xs font-bold tracking-[1.5px] text-white uppercase no-underline transition-colors"
        >
          Read the privacy model
          <ArrowRight size={15} />
        </a>
      </div>

      <div class="grid min-w-0 gap-4 sm:grid-cols-2">
        {#each principles as principle}
          <article
            class="principle-card flex min-h-[220px] flex-col border-[2px] border-white/25 bg-white/[0.045] p-6"
            style="--principle-color: {principle.color}"
          >
            <div
              class="flex h-11 w-11 items-center justify-center border-[2px] border-white/40 bg-[var(--principle-color)] text-[var(--text-primary)]"
            >
              <svelte:component this={principle.icon} size={21} strokeWidth={2.3} />
            </div>
            <h3 class="font-space mt-7 text-xl font-black tracking-[0.4px] uppercase">
              {principle.title}
            </h3>
            <p class="mt-3 text-sm leading-[1.7] text-white/60">{principle.text}</p>
            <span class="mt-auto block h-1 w-10 bg-[var(--principle-color)]"></span>
          </article>
        {/each}
      </div>
    </div>

    <div
      class="mt-16 overflow-hidden border-[3px] border-white/30 bg-[#202020] shadow-[8px_8px_0_var(--nogon-indigo)]"
    >
      <div
        class="flex flex-wrap items-center justify-between gap-2 border-b-[2px] border-white/20 px-5 py-3"
      >
        <span class="font-fira text-[10px] font-bold tracking-[1.4px] text-white/65 uppercase"
          >Your private search path</span
        >
      </div>
      <div class="grid items-stretch md:grid-cols-[1fr_auto_1fr_auto_1fr]">
        {#each [{ icon: EyeOff, label: 'Your sources', copy: 'Pages, files, and history you choose', color: 'bg-nogon-coral' }, { icon: Server, label: 'Your Nogon server', copy: 'Extraction, rules, and searchable index', color: 'bg-nogon-indigo' }, { icon: Check, label: 'Your answers', copy: 'Results, previews, and integrations', color: 'bg-nogon-lime' }] as stage, i}
          <article class="flex items-center gap-4 p-6 md:p-8">
            <div
              class="flex h-12 w-12 shrink-0 items-center justify-center border-[2px] border-white/35 {stage.color} {i ===
              2
                ? 'text-[var(--text-primary)]'
                : 'text-white'}"
            >
              <svelte:component this={stage.icon} size={22} />
            </div>
            <div>
              <h3 class="font-space text-sm font-black tracking-[1px] uppercase">{stage.label}</h3>
              <p class="mt-1 text-xs leading-relaxed text-white/65">{stage.copy}</p>
            </div>
          </article>
          {#if i < 2}
            <div class="flow-rule flex items-center justify-center" aria-hidden="true">
              <ArrowRight size={20} class="hidden text-white/50 md:block" />
            </div>
          {/if}
        {/each}
      </div>
    </div>

    <p class="font-fira mt-6 max-w-[1100px] text-[10px] leading-relaxed text-white/65">
      Optional semantic search sends text to the embeddings endpoint you configure. Browser
      extensions may retrieve page favicons. You choose whether and where these connections run.
    </p>
  </div>
</section>

<style>
  .principle-card {
    position: relative;
    transition:
      border-color 180ms ease,
      background-color 180ms ease,
      translate 180ms ease;
  }

  .principle-card:hover {
    border-color: var(--principle-color);
    background: color-mix(in srgb, var(--principle-color) 10%, transparent);
    translate: 0 -3px;
  }

  .flow-rule {
    min-height: 3px;
    background: color-mix(in srgb, white 18%, transparent);
  }

  @media (min-width: 768px) {
    .flow-rule {
      min-width: 3px;
      background: transparent;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .principle-card {
      transition: none;
    }
  }
</style>
