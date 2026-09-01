<script lang="ts">
  import Seo from '$lib/Seo.svelte';
</script>

<Seo
  title="Hister Bot | Crawler Info"
  description="Information about the Hister web crawler: what it does, how to control it, and how to contact us."
  path="/bot"
/>

<main class="mx-auto max-w-3xl px-6 py-16">
  <h1 class="mb-4 text-3xl font-bold">Hister Crawler</h1>

  <p class="mb-8 text-lg text-gray-600 dark:text-gray-400">
    Hister is a self-hosted personal search engine. Its crawler indexes web
    pages on behalf of individual users running their own Hister instances. It
    is not a commercial bot and does not sell or aggregate data centrally.
  </p>

  <section class="mb-10">
    <h2 class="mb-3 text-xl font-semibold">What the crawler does</h2>
    <ul class="list-disc space-y-2 pl-6 text-gray-700 dark:text-gray-300">
      <li>Follows links from pages the user has asked Hister to index.</li>
      <li>Respects <code class="font-mono text-sm">robots.txt</code> rules, including <code class="font-mono text-sm">Crawl-delay</code>.</li>
      <li>Identifies itself with a <code class="font-mono text-sm">User-agent</code> header starting with <code class="font-mono text-sm">Hister/</code>.</li>
      <li>Only stores content locally on the user's own machine — nothing is sent to a central server.</li>
    </ul>
  </section>

  <section class="mb-10">
    <h2 class="mb-3 text-xl font-semibold">How to block the crawler</h2>
    <p class="mb-4 text-gray-700 dark:text-gray-300">
      Add the following to your <code class="font-mono text-sm">robots.txt</code> to block Hister from crawling your site:
    </p>
    <pre class="overflow-x-auto rounded-lg bg-gray-100 p-4 font-mono text-sm dark:bg-gray-800">
User-agent: Hister
Disallow: /</pre>
    <p class="mt-4 text-gray-700 dark:text-gray-300">
      You can also restrict specific paths:
    </p>
    <pre class="overflow-x-auto rounded-lg bg-gray-100 p-4 font-mono text-sm dark:bg-gray-800">
User-agent: Hister
Disallow: /private/
Disallow: /api/</pre>
  </section>

  <section class="mb-10">
    <h2 class="mb-3 text-xl font-semibold">Rate limiting and crawl delay</h2>
    <p class="text-gray-700 dark:text-gray-300">
      The Hister crawler honors the <code class="font-mono text-sm">Crawl-delay</code> directive in
      <code class="font-mono text-sm">robots.txt</code>. By default it limits itself to one request per second per
      host and respects any <code class="font-mono text-sm">Retry-After</code> headers your server sends.
    </p>
  </section>

  <section>
    <h2 class="mb-3 text-xl font-semibold">Contact</h2>
    <p class="text-gray-700 dark:text-gray-300">
      Hister is open-source software. If you have questions or concerns about a
      specific instance crawling your site, the user running that instance
      configured a contact URL in their setup. You can also open an issue on the
      <a
        href="https://github.com/asciimoo/hister"
        class="underline hover:text-blue-600"
        rel="noopener noreferrer"
        target="_blank">Hister GitHub repository</a
      >.
    </p>
  </section>
</main>
