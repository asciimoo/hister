<script lang="ts">
  import { Button } from '@nogon/components/ui/button';
  import { Badge } from '@nogon/components/ui/badge';
  import { Separator } from '@nogon/components/ui/separator';
  import { Clock, Calendar, Eye, ThumbsUp, ExternalLink } from '@lucide/svelte';

  interface Chapter {
    title: string;
    startTime: string;
  }

  interface Playlist {
    title: string;
    index: number;
    count: number;
  }

  interface VideoData {
    title: string;
    uploader: string;
    duration: number;
    durationFormatted: string;
    uploadDate: string;
    viewCount: number;
    likeCount: number;
    categories: string[];
    tags: string[];
    description: string;
    thumbnail: string;
    chapters?: Chapter[];
    playlist?: Playlist;
    transcript?: string;
    webpageUrl: string;
  }

  let { data }: { data: VideoData } = $props();

  let showTranscript = $state(false);

  function formatNumber(n: number): string {
    if (n >= 1_000_000_000) return (n / 1_000_000_000).toFixed(1) + 'B';
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
    if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K';
    return n.toString();
  }
</script>

<article class="not-prose font-inter space-y-4">
  {#if data.thumbnail}
    <figure>
      <img
        src={data.thumbnail}
        alt={data.title}
        class="border-brutal-border w-full border-2 object-cover"
      />
      <figcaption class="mt-2">
        <Button
          variant="outline"
          class="brutal-press font-outfit w-full text-sm font-bold tracking-wide uppercase"
          onclick={() => window.open(data.webpageUrl, '_blank', 'noopener,noreferrer')}
        >
          <ExternalLink class="mr-1 size-4" />
          Open Externally
        </Button>
      </figcaption>
    </figure>
  {/if}

  {#if data.uploader}
    <p class="font-outfit text-text-brand text-base font-bold">{data.uploader}</p>
  {/if}

  <dl class="text-text-brand-secondary flex flex-wrap gap-x-4 gap-y-1.5 text-xs">
    {#if data.durationFormatted}
      <div class="flex items-center gap-1">
        <Clock class="text-nogon-teal size-3.5" />
        <dt class="sr-only">Duration</dt>
        <dd>{data.durationFormatted}</dd>
      </div>
    {/if}
    {#if data.uploadDate}
      <div class="flex items-center gap-1">
        <Calendar class="text-nogon-teal size-3.5" />
        <dt class="sr-only">Uploaded</dt>
        <dd>{data.uploadDate}</dd>
      </div>
    {/if}
    {#if data.viewCount}
      <div class="flex items-center gap-1">
        <Eye class="text-nogon-teal size-3.5" />
        <dt class="sr-only">Views</dt>
        <dd>{formatNumber(data.viewCount)} views</dd>
      </div>
    {/if}
    {#if data.likeCount}
      <div class="flex items-center gap-1">
        <ThumbsUp class="text-nogon-teal size-3.5" />
        <dt class="sr-only">Likes</dt>
        <dd>{formatNumber(data.likeCount)} likes</dd>
      </div>
    {/if}
  </dl>

  {#if data.playlist}
    <aside class="bg-muted-surface border-border-brand border px-3 py-1.5 text-xs">
      <strong>Playlist:</strong>
      {data.playlist.title}
      {#if data.playlist.index && data.playlist.count}
        <span class="text-text-brand-muted">({data.playlist.index}/{data.playlist.count})</span>
      {/if}
    </aside>
  {/if}

  {#if data.categories?.length || data.tags?.length}
    <nav class="flex flex-wrap gap-1.5" aria-label="Video tags">
      {#each data.categories || [] as cat}
        <Badge
          variant="secondary"
          class="bg-nogon-indigo/15 text-nogon-indigo border-nogon-indigo/30 text-[11px]"
          >{cat}</Badge
        >
      {/each}
      {#each data.tags || [] as tag}
        <Badge variant="outline" class="text-nogon-teal border-nogon-teal/30 text-[11px]"
          >{tag}</Badge
        >
      {/each}
    </nav>
  {/if}

  {#if data.chapters?.length}
    <Separator />
    <section>
      <h3 class="font-outfit text-text-brand mb-2 text-sm font-bold tracking-wide uppercase">
        Chapters
      </h3>
      <ol class="list-none space-y-0.5 pl-0">
        {#each data.chapters as ch}
          <li class="text-text-brand-secondary flex gap-3 text-xs">
            <code class="text-nogon-teal w-14 shrink-0">{ch.startTime}</code>
            <span class="text-text-brand">{ch.title}</span>
          </li>
        {/each}
      </ol>
    </section>
  {/if}

  <!-- Description -->
  {#if data.description}
    <Separator />
    <section>
      <h3 class="font-outfit text-text-brand mb-2 text-sm font-bold tracking-wide uppercase">
        Description
      </h3>
      <p class="text-text-brand-secondary text-xs leading-relaxed whitespace-pre-wrap">
        {data.description}
      </p>
    </section>
  {/if}

  {#if data.transcript}
    <Separator />
    <section>
      <Button
        variant="link"
        class="font-outfit text-text-brand p-0 text-sm font-bold tracking-wide uppercase"
        onclick={() => (showTranscript = !showTranscript)}
      >
        {showTranscript ? 'Hide Transcript' : 'Show Transcript'}
      </Button>
      {#if showTranscript}
        <p
          class="text-text-brand-secondary mt-2 max-h-80 overflow-y-auto text-xs leading-relaxed whitespace-pre-wrap"
        >
          {data.transcript}
        </p>
      {/if}
    </section>
  {/if}
</article>
