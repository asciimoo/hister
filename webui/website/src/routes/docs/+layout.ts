import { docsStructure } from '$lib/docs-structure.js';

const modules = import.meta.glob('../../content/docs/*.md', { eager: true });

export interface DocEntry {
  slug: string;
  title: string;
}

export interface DocCategory {
  name: string;
  docs: DocEntry[];
}

export async function load() {
  const docs: DocEntry[] = docsStructure.flatMap(({ slugs }) =>
    slugs.map((slug) => {
      const mod = modules[`../../content/docs/${slug}.md`] as { metadata?: Record<string, string> };
      return {
        slug,
        title:
          mod?.metadata?.title ?? slug.replace(/-/g, ' ').replace(/\b\w/g, (l) => l.toUpperCase()),
      };
    }),
  );

  const categories: DocCategory[] = docsStructure
    .map(({ name, slugs }) => ({
      name,
      docs: docs.filter((d) => slugs.includes(d.slug)),
    }))
    .filter((c) => c.docs.length > 0);

  return { docs, categories };
}
