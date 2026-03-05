import adapter from '@sveltejs/adapter-static';
import { mdsvex } from 'mdsvex';
import rehypeSlug from 'rehype-slug';

/** @type {import('@sveltejs/kit').Config} */
export default {
  extensions: ['.svelte', '.md', '.svx'],
  preprocess: [mdsvex({ extensions: ['.md', '.svx'], rehypePlugins: [rehypeSlug] })],
  kit: {
    adapter: adapter({ pages: 'build', assets: 'build', fallback: undefined }),
    prerender: { handleHttpError: 'warn', handleMissingId: 'ignore' },
  },
};
