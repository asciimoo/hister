import adapter from '@sveltejs/adapter-static';

const dev = process.env.NODE_ENV === 'development';

/** @type {import('@sveltejs/kit').Config} */
export default {
  kit: {
    paths: {
      base: dev ? '' : '/magic-string-that-we-replace-runtime-in-the-app',
    },
    adapter: adapter({
      pages: 'build',
      assets: 'build',
      fallback: 'index.html',
    }),
  },
};
