<svelte:head>
  <title>Hister - About</title>
</svelte:head>

<div class="px-4 md:px-12 py-6 md:py-10 overflow-y-auto flex-1">
  <div class="max-w-3xl mx-auto">
    <h1 class="flex items-center gap-3 mb-8">
      <span class="w-1.5 h-8 bg-hister-coral"></span><span
        class="font-space text-2xl md:text-3xl tracking-[2px] font-extrabold text-text-brand uppercase"
        >About Hister</span
      >
    </h1>
  </div>
  <div
    class="prose prose-neutral dark:prose-invert max-w-3xl mx-auto prose-headings:font-outfit prose-headings:font-extrabold prose-h1:text-text-brand prose-h2:text-text-brand prose-p:font-inter prose-p:text-text-brand-secondary prose-li:font-inter prose-li:text-text-brand-secondary prose-a:text-hister-indigo prose-a:no-underline hover:prose-a:underline prose-strong:text-text-brand prose-hr:border-border-brand-muted"
  >
    <p>
      Hister is a web history management tool that provides blazing fast, content-based search for
      visited websites. Unlike traditional browser history that only searches URLs and titles,
      Hister indexes the full content of web pages you visit, enabling deep and meaningful search
      across your browsing history.
    </p>

    <h2>Features</h2>

    <p>
      Traditional browser history features are limited to basic keyword matching on URLs and page
      titles. Hister goes beyond these limitations by:
    </p>

    <ul>
      <li>
        <strong>Privacy-focused</strong>: Keep your browsing history indexed locally on your machine
      </li>
      <li>
        <strong>Full-text indexing</strong>: Search through the actual content of web pages you've
        visited
      </li>
      <li>
        <strong>Advanced search capabilities</strong>: Utilize a powerful query language for precise
        results
      </li>
      <li>
        <strong>Smart organization</strong>: Configure blacklist and priority rules for better
        control
      </li>
      <li><strong>Efficient retrieval</strong>: Use keyword aliases to quickly find content</li>
      <li>
        <strong>Offline preview</strong>: Open clutter-free, offline preview of result web pages
      </li>
    </ul>

    <h2>Why Hister?</h2>

    <p>When we search online it can be because of two reasons:</p>
    <ul>
      <li><strong>Find new information</strong></li>
      <li><strong>Recall information we've read before</strong></li>
    </ul>
    <p>
      Hister aids you with the latter by indexing all the websites you visit and providing a search
      interface as efficient as possible, while seamlessly letting you fall back to the traditional
      search methods in case you need to find unexplored content.
    </p>

    <hr />
    <p>Benefits of using Hister instead of free online search engines:</p>
    <ul>
      <li>Privacy</li>
      <li>No advertisements</li>
      <li>No incorrect AI suggestion</li>
      <li>No sponsored results</li>
      <li>No manipulated result order</li>
    </ul>

    <h2>Use Cases</h2>

    <p>Hister is perfect for:</p>
    <ul>
      <li><strong>Privacy protection</strong>: Manage your data, access it without tracking</li>
      <li>
        <strong>Research</strong>: Quickly find information from articles and documentation you've
        read
      </li>
      <li>
        <strong>Reference</strong>: Locate code snippets, tutorials, or solutions you've visited
        before
      </li>
      <li>
        <strong>Knowledge management</strong>: Build a searchable archive of your web browsing
      </li>
      <li>
        <strong>Productivity</strong>: Spend less time searching for "that page I saw last week"
      </li>
    </ul>

    <h2>Technology</h2>

    <p>
      Hister is built with Go and uses the Bleve search engine for lightning-fast full-text search
      capabilities.<br />
      The application consists of:
    </p>

    <ul>
      <li>A local web server for search interface</li>
      <li>
        Browser extensions for <a
          href="https://chromewebstore.google.com/detail/hister/cciilamhchpmbdnniabclekddabkifhb"
          >Chrome</a
        >
        and <a href="https://addons.mozilla.org/en-US/firefox/addon/hister/">Firefox</a> to automatically
        index visited pages
      </li>
      <li>A command line utility to index/import/explore the contents of Hister</li>
    </ul>

    <h2>License</h2>

    <p>Hister is free and open-source software licensed under AGPLv3.</p>

    <h2>Community</h2>

    <p>
      Found a bug or have a suggestion? Visit the <a
        href="https://github.com/asciimoo/hister/issues">issue tracker</a
      > on GitHub.
    </p>
  </div>
</div>
