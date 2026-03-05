<script lang="ts">
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import Github from '@lucide/svelte/icons/github';
  import { Button } from '@hister/components';
</script>

<section class="w-full bg-brutal-bg px-6 py-12 md:py-24 flex flex-col items-center gap-8">
  <h1
    class="font-outfit text-5xl md:text-7xl lg:text-[100px] font-black text-center leading-[0.95] tracking-[-2px] text-[var(--text-primary)] max-w-[1000px] uppercase"
  >
    Your Own <br /> Search Engine
  </h1>

  <p
    class="font-inter text-base md:text-lg text-center leading-relaxed text-[var(--text-secondary)] max-w-[700px]"
  >
    Hister automatically indexes every page you visit.<br />
    Full-text search across your entire browsing history and beyond.<br />
    Private. Fast. Yours.
  </p>

  <nav class="flex flex-wrap items-center justify-center gap-4">
    <Button
      href="https://demo.hister.org/"
      class="bg-hister-indigo hover:bg-hister-indigo/90 text-white font-space text-lg font-bold tracking-[1.5px] px-10 md:px-14 py-5 h-auto border-[3px] border-brutal-border shadow-[5px_5px_0_var(--brutal-shadow)] no-underline hover:shadow-[2px_2px_0_var(--brutal-shadow)] hover:translate-x-[3px] hover:translate-y-[3px] transition-all rounded-none"
    >
      TRY THE DEMO
      <ArrowRight size={20} />
    </Button>
    <Button
      href="https://github.com/asciimoo/hister"
      target="_blank"
      rel="noopener noreferrer"
      class="bg-brutal-card hover:bg-brutal-card text-[var(--text-primary)] font-space text-lg font-bold tracking-[1.5px] px-10 md:px-14 py-5 h-auto border-[3px] border-brutal-border shadow-[5px_5px_0_var(--brutal-shadow)] no-underline hover:shadow-[2px_2px_0_var(--brutal-shadow)] hover:translate-x-[3px] hover:translate-y-[3px] transition-all rounded-none"
    >
      VIEW ON GITHUB
      <Github size={20} />
    </Button>
  </nav>
</section>
