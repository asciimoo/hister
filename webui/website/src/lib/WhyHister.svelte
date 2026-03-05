<script lang="ts">
  import XIcon from '@lucide/svelte/icons/x';
  import { Badge } from '@hister/components';

  const noMore = [
    'Advertisements',
    'Incorrect AI Suggestions',
    'Sponsored Results',
    'Manipulated Result Order',
    'Privacy Concerns',
  ];
</script>

<section class="w-full bg-page-bg">
  <div class="grid grid-cols-1 md:grid-cols-2">
    <article class="bg-[var(--text-primary)] p-8 md:p-12 flex flex-col gap-6">
      <h2
        class="text-white font-space text-4xl md:text-[56px] font-black tracking-[-1px] leading-[0.95] uppercase"
      >
        Why Hister?
      </h2>
      <blockquote class="p-5 md:p-6">
        <p class="text-white font-inter md:text-2xl font-medium leading-[1.7]">
          Keep your data private and local while enjoying powerful search capabilities.
        </p>
      </blockquote>
    </article>

    <aside class="bg-hister-indigo p-8 md:p-12 flex flex-col gap-6 min-h-[430px]">
      <Badge
        class="bg-white/10 text-white font-space md:text-lg font-bold tracking-[2px] px-3.5 py-1.5 w-fit rounded-none border-none uppercase"
      >
        Benefits
      </Badge>
      <h2 class="text-white font-space text-4xl font-extrabold tracking-[2px] uppercase">
        No more
      </h2>
      <ul class="flex flex-col gap-3 list-none p-0 m-0">
        {#each noMore as item}
          <li class="flex items-center gap-3.5 bg-white/5 border border-white/15 px-4 py-3.5">
            <XIcon size={18} class="text-hister-white shrink-0" />
            <span class="text-white font-space text-[15px] font-bold tracking-[1px] uppercase"
              >{item}</span
            >
          </li>
        {/each}
      </ul>
    </aside>
  </div>
</section>
