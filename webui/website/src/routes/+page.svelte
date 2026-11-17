<script lang="ts">
  import Hero from '#lib/Hero.svelte';
  import HowItWorks from '#lib/HowItWorks.svelte';
  import Previews from '#lib/Previews.svelte';
  import WhyNogon from '#lib/WhyNogon.svelte';
  import Features from '#lib/Features.svelte';
  import FinalCTA from '#lib/FinalCTA.svelte';
  import Seo from '#lib/Seo.svelte';
</script>

<Seo
  title="Nogon | Your Own Search Engine"
  description="Nogon is a private, self hosted search engine for the pages you visit and the files you keep. Search from the web, terminal, command line, or MCP."
  path="/"
  schemaType="SoftwareApplication"
  schemaTitle="Nogon"
/>

<Hero />
<FinalCTA />
<Previews />
<HowItWorks />
<WhyNogon />
<Features />
