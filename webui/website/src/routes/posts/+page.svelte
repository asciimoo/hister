<script lang="ts">
  import * as Card from '@hister/components/ui/card';

  let { data } = $props();
</script>

<svelte:head>
  <title>Posts | Hister</title>
</svelte:head>

<section class="max-w-4xl mx-auto px-6 md:px-12 py-12">
  <h1 class="font-space text-4xl md:text-5xl font-black tracking-[-1px] text-[var(--text-primary)] mb-10">POSTS</h1>

  <ul class="flex flex-col gap-4 list-none m-0 p-0">
    {#each data.posts as post}
      <li>
        <a href="/posts/{post.slug}/" class="block no-underline hover:translate-x-[3px] hover:translate-y-[3px] transition-all group">
          <Card.Root class="bg-brutal-card border-[3px] border-brutal-border shadow-[6px_6px_0_var(--brutal-shadow)] group-hover:shadow-[3px_3px_0_var(--brutal-shadow)] rounded-none p-0 gap-0 transition-shadow">
            <Card.Content class="p-6">
              <Card.Title class="font-space text-xl font-extrabold tracking-[0.5px] text-[var(--text-primary)] mb-2">{post.title}</Card.Title>
              {#if post.date}
                <Card.Description>
                  <time class="font-inter text-sm text-[var(--text-secondary)]" datetime={post.date}>
                    {new Date(post.date).toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' })}
                  </time>
                </Card.Description>
              {/if}
            </Card.Content>
          </Card.Root>
        </a>
      </li>
    {/each}
  </ul>
</section>
