import type { RequestHandler } from '@sveltejs/kit';
import { SITE_URL } from '#lib/config.js';

const FEED_TITLE = 'Nogon Posts';
const FEED_DESCRIPTION = 'Latest posts from the Nogon blog';
// Keep published item IDs stable so a domain change does not duplicate old posts.
const FEED_ID_ORIGIN = 'https://hister.org';

const modules = import.meta.glob('../../../content/posts/*.md', { eager: true });

function escapeXml(str: string): string {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;');
}

export const prerender = true;

export const GET: RequestHandler = () => {
  const posts = Object.entries(modules)
    .map(([path, mod]) => {
      const slug = path.split('/').pop()?.replace('.md', '') ?? path;
      const { metadata } = mod as {
        metadata?: { date?: string; title?: string; description?: string; draft?: boolean };
      };
      return {
        slug,
        title: metadata?.title ?? slug,
        description: metadata?.description ?? '',
        date: metadata?.date ?? '',
        draft: metadata?.draft ?? false,
      };
    })
    .filter((p) => !p.draft && p.date)
    .sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime());

  const lastBuildDate =
    posts.length > 0 ? new Date(posts[0].date).toUTCString() : new Date().toUTCString();

  const items = posts
    .map((post) => {
      const url = `${SITE_URL}/posts/${post.slug}`;
      const guid = `${FEED_ID_ORIGIN}/posts/${post.slug}`;
      const pubDate = new Date(post.date).toUTCString();
      return `
    <item>
      <title>${escapeXml(post.title)}</title>
      <link>${url}</link>
      <guid isPermaLink="true">${guid}</guid>
      <pubDate>${pubDate}</pubDate>${post.description ? `\n      <description>${escapeXml(post.description)}</description>` : ''}
    </item>`;
    })
    .join('');

  const xml = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>${escapeXml(FEED_TITLE)}</title>
    <link>${SITE_URL}/posts</link>
    <description>${escapeXml(FEED_DESCRIPTION)}</description>
    <language>en</language>
    <lastBuildDate>${lastBuildDate}</lastBuildDate>
    <atom:link href="${SITE_URL}/posts/rss.xml" rel="self" type="application/rss+xml" />${items}
  </channel>
</rss>`;

  return new Response(xml, {
    headers: {
      'Content-Type': 'application/rss+xml; charset=utf-8',
      'Cache-Control': 'max-age=3600',
    },
  });
};
