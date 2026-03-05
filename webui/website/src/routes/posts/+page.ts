const modules = import.meta.glob('../../content/posts/*.md', { eager: true });

export async function load() {
  const posts = Object.entries(modules)
    .map(([path, mod]) => {
      const slug = path.split('/').pop()?.replace('.md', '') ?? path;
      const { metadata } = mod as { metadata?: { date: string; title: string } };
      return {
        slug,
        title: metadata?.title ?? slug,
        date: metadata?.date,
      };
    })
    .sort((a, b) => new Date(b.date ?? 0).getTime() - new Date(a.date ?? 0).getTime());

  return { posts };
}
