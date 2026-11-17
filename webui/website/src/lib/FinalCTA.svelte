<script lang="ts">
  import Download from '@lucide/svelte/icons/download';
  import ExternalLink from '@lucide/svelte/icons/external-link';
  import { Button } from '@nogon/components';
</script>

<section
  class="border-brutal-border bg-nogon-coral relative isolate overflow-hidden border-b-[3px]"
>
  <div class="document-backdrop" aria-hidden="true">
    <svg viewBox="0 0 1600 700" preserveAspectRatio="xMidYMid slice">
      <g class="document-fragment" transform="translate(-135 95) rotate(-5 230 135)">
        <path class="sheet-back" d="M 24 18 H 478 V 282 H 24 Z" />
        <path class="sheet" d="M 0 0 H 410 L 460 50 V 264 H 0 Z" />
        <path class="fold" d="M 410 0 V 50 H 460" />
        <path
          class="copy"
          d="M 42 50 H 246 M 42 82 H 358 M 42 114 H 286 M 42 146 H 380 M 42 178 H 318 M 42 210 H 350"
        />
        <rect class="match" x="38" y="132" width="218" height="24" />
      </g>

      <g
        class="document-fragment document-fragment-muted"
        transform="translate(470 -155) rotate(4 250 145)"
      >
        <path class="sheet-back" d="M 22 18 H 512 V 300 H 22 Z" />
        <path class="sheet" d="M 0 0 H 442 L 492 50 V 282 H 0 Z" />
        <path class="fold" d="M 442 0 V 50 H 492" />
        <path
          class="copy"
          d="M 44 54 H 282 M 44 88 H 402 M 44 122 H 332 M 44 156 H 420 M 44 190 H 286 M 44 224 H 382"
        />
        <rect class="match" x="40" y="108" width="252" height="26" />
      </g>

      <g class="document-fragment" transform="translate(1280 -45) rotate(7 230 140)">
        <path class="sheet-back" d="M 24 18 H 476 V 292 H 24 Z" />
        <path class="sheet" d="M 0 0 H 404 L 454 50 V 274 H 0 Z" />
        <path class="fold" d="M 404 0 V 50 H 454" />
        <path
          class="copy"
          d="M 42 52 H 256 M 42 84 H 352 M 42 116 H 298 M 42 148 H 374 M 42 180 H 274 M 42 212 H 338"
        />
        <rect class="match" x="38" y="166" width="202" height="24" />
      </g>

      <g class="document-fragment" transform="translate(-80 545) rotate(3 250 145)">
        <path class="sheet-back" d="M 24 18 H 518 V 300 H 24 Z" />
        <path class="sheet" d="M 0 0 H 446 L 496 50 V 282 H 0 Z" />
        <path class="fold" d="M 446 0 V 50 H 496" />
        <path
          class="copy"
          d="M 44 54 H 300 M 44 88 H 414 M 44 122 H 340 M 44 156 H 392 M 44 190 H 286 M 44 224 H 422"
        />
        <rect class="match" x="40" y="74" width="238" height="26" />
      </g>

      <g class="document-fragment" transform="translate(1110 515) rotate(-4 260 145)">
        <path class="sheet-back" d="M 24 18 H 528 V 310 H 24 Z" />
        <path class="sheet" d="M 0 0 H 456 L 506 50 V 292 H 0 Z" />
        <path class="fold" d="M 456 0 V 50 H 506" />
        <path
          class="copy"
          d="M 44 54 H 284 M 44 88 H 422 M 44 122 H 354 M 44 156 H 404 M 44 190 H 312 M 44 224 H 430"
        />
        <rect class="match" x="40" y="142" width="274" height="26" />
      </g>
    </svg>
  </div>
  <div class="relative z-10 mx-auto w-full max-w-[1500px] px-6 py-16 md:px-12 md:py-24">
    <div class="mx-auto flex max-w-[900px] min-w-0 flex-col items-center text-center">
      <h2
        class="font-outfit max-w-[880px] text-4xl leading-[0.88] font-black tracking-[-0.05em] text-[var(--text-primary)] uppercase md:text-7xl lg:text-8xl"
      >
        Preserve Knowledge.<br />Find It Again.
      </h2>
      <p class="mt-7 max-w-[650px] text-lg leading-[1.75] text-[#1f1f1f]">
        Nogon stores extracted document content with the search index and displays it as a readable
        preview alongside search results.
      </p>

      <div class="mt-9 flex flex-col flex-wrap justify-center gap-4 sm:flex-row">
        <Button
          href="https://github.com/asciimoo/hister/releases/latest"
          target="_blank"
          rel="noopener noreferrer"
          class="font-space brutal-press-lg border-brutal-border h-auto justify-center rounded-none border-[3px] bg-[var(--text-primary)] px-8 py-4 text-[15px] font-bold tracking-[1.3px] text-white uppercase no-underline"
        >
          Download Nogon
          <Download size={18} />
        </Button>
        <Button
          href="https://demo.hister.org/"
          target="_blank"
          rel="noopener noreferrer"
          class="bg-brutal-card font-space brutal-press-lg border-brutal-border h-auto justify-center rounded-none border-[3px] px-8 py-4 text-[15px] font-bold tracking-[1.3px] text-[var(--text-primary)] uppercase no-underline hover:bg-[var(--text-primary)] hover:text-white"
        >
          Try the live demo
          <ExternalLink size={18} />
        </Button>
      </div>
    </div>
  </div>
</section>

<style>
  .document-backdrop {
    position: absolute;
    inset: 0;
    pointer-events: none;
    opacity: 0.09;
  }

  .document-backdrop svg {
    width: 100%;
    height: 100%;
  }

  .document-fragment .sheet,
  .document-fragment .sheet-back {
    fill: color-mix(in srgb, var(--brutal-card) 58%, transparent);
    stroke: var(--brutal-border);
    stroke-width: 4;
    vector-effect: non-scaling-stroke;
  }

  .document-fragment .sheet-back {
    fill: none;
  }

  .document-fragment .fold,
  .document-fragment .copy {
    fill: none;
    stroke: var(--brutal-border);
    stroke-width: 6;
    vector-effect: non-scaling-stroke;
  }

  .document-fragment .match {
    fill: var(--nogon-lime);
    stroke: none;
  }

  .document-fragment-muted {
    opacity: 0.7;
  }

  @media (max-width: 767px) {
    .document-backdrop {
      opacity: 0.07;
      transform: scale(1.2);
    }
  }
</style>
