<script lang="ts">
  let { data } = $props();
</script>

<svelte:head>
  <title>{data.meta.title} | Hister - Web History on Steroids</title>
  {#if data.meta.description}
    <meta name="description" content={data.meta.description as string} />
  {/if}
  <meta property="og:title" content={data.meta.title} />
  {#if data.meta.description}
    <meta property="og:description" content={data.meta.description as string} />
  {/if}
  <meta property="og:type" content="article" />
  <meta property="article:section" content="Post" />
</svelte:head>

<header class="w-full bg-[var(--text-primary)] px-6 py-12 md:py-16">
  <div class="max-w-3xl mx-auto">
    {#if data.date}
      <time class="font-inter text-sm text-white/40" datetime={data.date}>{data.date}</time>
    {/if}
    <h1
      class="font-space text-3xl md:text-5xl font-black text-white tracking-[-1px] leading-tight mt-3"
    >
      {data.meta.title}
    </h1>
  </div>
</header>

<article class="max-w-3xl mx-auto px-6 py-12">
  <div class="content">
    <data.content />
  </div>
</article>
