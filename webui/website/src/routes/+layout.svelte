<script lang="ts">
  import '../app.css';
  import { afterNavigate } from '$app/navigation';
  import Header from '#lib/Header.svelte';
  import Footer from '#lib/Footer.svelte';

  let { children } = $props();

  afterNavigate(({ shallow }) => {
    if (shallow) return;

    const hash = window.location.hash;
    if (hash) {
      let id: string;
      try {
        id = decodeURIComponent(hash.slice(1));
      } catch {
        return;
      }
      document.getElementById(id)?.scrollIntoView();
    }
  });
</script>

<a
  href="#main-content"
  class="border-brutal-border bg-brutal-card shadow-brutal sr-only fixed top-4 left-4 z-50 border-[3px] px-4 py-3 font-bold text-(--text-primary) focus:not-sr-only"
>
  Skip to main content
</a>

<div class="bg-brutal-bg flex min-h-screen flex-col">
  <Header />
  <main id="main-content" tabindex="-1" class="flex-1 outline-none">
    {@render children()}
  </main>
  <Footer />
</div>
