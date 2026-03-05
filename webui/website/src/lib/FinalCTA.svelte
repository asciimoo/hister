<script lang="ts">
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import { Button } from '@hister/components';
</script>

<section
  class="w-full bg-[var(--text-primary)] border-t-[3px] border-hister-indigo px-6 py-16 md:py-24 flex flex-col items-center gap-10"
>
  <h2
    class="font-space text-4xl md:text-6xl lg:text-[64px] font-black text-white text-center leading-[0.95] tracking-[-1px] max-w-[800px]"
  >
    READY TO OWN YOUR SEARCH?
  </h2>

  <p class="text-white/55 font-inter text-lg text-center leading-relaxed max-w-[600px]">
    Start indexing your browsing history today. Free, open-source, and completely private.
  </p>

  <nav class="flex flex-wrap items-center justify-center gap-4">
    <Button
      href="/docs"
      class="bg-hister-coral hover:bg-hister-coral/90 text-white font-space text-lg font-bold tracking-[1.5px] px-8 md:px-12 py-4 h-auto border-[3px] border-brutal-border shadow-[5px_5px_0_var(--hister-coral)] no-underline hover:shadow-[2px_2px_0_var(--hister-coral)] hover:translate-x-[3px] hover:translate-y-[3px] transition-all rounded-none"
    >
      GET STARTED
      <ArrowRight size={22} />
    </Button>
    <Button
      href="/docs"
      class="bg-white/10 hover:bg-white/15 text-white font-space text-lg font-bold tracking-[1.5px] px-8 md:px-12 py-4 h-auto border-2 border-white/25 shadow-[5px_5px_0_rgba(255,255,255,0.12)] no-underline hover:shadow-[2px_2px_0_rgba(255,255,255,0.12)] hover:translate-x-[3px] hover:translate-y-[3px] transition-all rounded-none"
    >
      DOCUMENTATION
    </Button>
  </nav>
</section>
