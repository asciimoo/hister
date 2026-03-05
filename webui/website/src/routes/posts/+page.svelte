<script lang="ts">
  let { data } = $props();
</script>

<svelte:head>
  <title>Posts | Hister</title>
</svelte:head>

<section class="max-w-4xl mx-auto px-6 md:px-12 py-12">
  <h1
    class="font-space text-4xl md:text-5xl font-black tracking-[-1px] text-[var(--text-primary)] mb-10"
  >
    POSTS
  </h1>

  <ul class="flex flex-col gap-4 list-none m-0 p-0">
    {#each data.posts as post}
      <li>
        <a
          href="/posts/{post.slug}/"
          class="block p-6 bg-brutal-card border-[3px] border-brutal-border shadow-[6px_6px_0_var(--brutal-shadow)] no-underline hover:shadow-[3px_3px_0_var(--brutal-shadow)] hover:translate-x-[3px] hover:translate-y-[3px] transition-all"
        >
          <h2
            class="font-space text-xl font-extrabold tracking-[0.5px] text-[var(--text-primary)] mb-2"
          >
            {post.title}
          </h2>
          {#if post.date}
            <time class="font-inter text-sm text-[var(--text-secondary)]" datetime={post.date}>
              {new Date(post.date).toLocaleDateString('en-US', {
                month: 'long',
                day: 'numeric',
                year: 'numeric',
              })}
            </time>
          {/if}
        </a>
      </li>
    {/each}
  </ul>
</section>
