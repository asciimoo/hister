<script lang="ts">
  import * as Card from '@nogon/components/ui/card';
  import Rss from '@lucide/svelte/icons/rss';
  import Seo from '#lib/Seo.svelte';

  let { data } = $props();
</script>

<Seo
  title="Posts | Hister"
  description="Read Hister news, guides, technical articles, and practical stories about private search and personal knowledge management."
  path="/posts"
/>

<section class="mx-auto max-w-4xl px-6 py-12 md:px-12">
  <div class="mb-10 flex items-center justify-between">
    <h1
      class="font-space text-4xl font-black tracking-[-1px] text-(--text-primary) uppercase md:text-5xl"
    >
      Posts
    </h1>
    <a
      href="/posts/rss.xml"
      title="RSS feed"
      aria-label="RSS feed"
      data-sveltekit-reload
      class="text-(--text-secondary) transition-colors hover:text-[#f26522]"
    >
      <Rss size={32} />
    </a>
  </div>

  <ul class="m-0 flex list-none flex-col gap-4 p-0">
    {#each data.posts as post}
      <li>
        <Card.Root href="/posts/{post.slug}" class="bg-brutal-card p-6">
          <h2 class="font-space mb-2 text-xl font-extrabold tracking-[0.5px] text-(--text-primary)">
            {post.title}
          </h2>
          {#if post.date}
            <time class="font-inter text-sm text-(--text-secondary)" datetime={post.date}>
              {new Date(post.date).toLocaleDateString('en-US', {
                month: 'long',
                day: 'numeric',
                year: 'numeric',
              })}
            </time>
          {/if}
          {#if post.description}
            <p class="font-inter mt-3 text-sm text-(--text-secondary)">
              {post.description}
            </p>
          {/if}
        </Card.Root>
      </li>
    {/each}
  </ul>
</section>
