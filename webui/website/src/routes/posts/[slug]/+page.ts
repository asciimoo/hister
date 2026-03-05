import { error } from '@sveltejs/kit';

export const prerender = true;

const modules = import.meta.glob('../../../content/posts/*.md', { eager: true });

const posts = Object.fromEntries(
  Object.entries(modules).map(([path, mod]) => {
    const slug = path.split('/').pop()?.replace('.md', '') ?? path;
    return [slug, mod];
  }),
) as Record<string, { default: unknown; metadata?: Record<string, unknown> }>;

export function entries() {
  return Object.keys(posts).map((slug) => ({ slug }));
}

export async function load({ params }: { params: { slug: string } }) {
  const post = posts[params.slug];
  if (!post) {
    error(404, `Post "${params.slug}" not found`);
  }
  const dateStr = post.metadata?.date as string | undefined;
  let formattedDate = '';
  if (dateStr) {
    const date = new Date(dateStr);
    formattedDate = date.toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
    });
  }
  return {
    content: post.default,
    meta: post.metadata ?? {},
    date: formattedDate,
  };
}
