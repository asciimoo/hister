<script lang="ts">
  import { SITE_URL } from '#lib/config.js';

  interface Breadcrumb {
    name: string;
    path: string;
  }

  interface Props {
    title: string;
    description: string;
    path: string;
    type?: 'website' | 'article';
    image?: string | null;
    imageAlt?: string;
    publishedTime?: string;
    modifiedTime?: string;
    author?: string;
    section?: string;
    schemaType?: 'SoftwareApplication' | 'BlogPosting' | 'TechArticle';
    schemaTitle?: string;
    breadcrumbs?: Breadcrumb[];
  }

  let {
    title,
    description,
    path,
    type = 'website',
    image = '/logo.png',
    imageAlt = 'Nogon logo',
    publishedTime,
    modifiedTime,
    author,
    section,
    schemaType,
    schemaTitle,
    breadcrumbs = [],
  }: Props = $props();

  const canonicalUrl = $derived(new URL(path, SITE_URL).href);
  const imageUrl = $derived(image ? new URL(image, SITE_URL).href : null);
  const twitterCard = $derived(!image || image === '/logo.png' ? 'summary' : 'summary_large_image');

  const organization = {
    '@type': 'Organization',
    '@id': `${SITE_URL}/#organization`,
    name: 'Nogon',
    url: SITE_URL,
    logo: {
      '@type': 'ImageObject',
      url: new URL('/logo.png', SITE_URL).href,
    },
  };

  const structuredData = $derived.by(() => {
    const graph: Record<string, unknown>[] = [];

    if (schemaType === 'SoftwareApplication') {
      graph.push({
        '@type': 'SoftwareApplication',
        '@id': `${canonicalUrl}#software`,
        name: schemaTitle ?? 'Nogon',
        description,
        url: canonicalUrl,
        applicationCategory: 'UtilitiesApplication',
        applicationSubCategory: 'Personal search engine',
        operatingSystem: ['Linux', 'macOS', 'Windows'],
        isAccessibleForFree: true,
        image: imageUrl,
        codeRepository: 'https://github.com/asciimoo/hister',
        downloadUrl: 'https://github.com/asciimoo/hister/releases/latest',
        softwareHelp: new URL('/docs', SITE_URL).href,
        license: 'https://www.gnu.org/licenses/agpl-3.0.html',
        author: organization,
        offers: {
          '@type': 'Offer',
          price: 0,
          priceCurrency: 'USD',
        },
      });
    } else if (schemaType === 'BlogPosting' || schemaType === 'TechArticle') {
      graph.push({
        '@type': schemaType,
        '@id': `${canonicalUrl}#article`,
        headline: schemaTitle ?? title,
        description,
        url: canonicalUrl,
        mainEntityOfPage: {
          '@type': 'WebPage',
          '@id': canonicalUrl,
        },
        image: imageUrl,
        datePublished: publishedTime,
        dateModified: modifiedTime ?? publishedTime,
        author: author
          ? {
              '@type': schemaType === 'BlogPosting' ? 'Person' : 'Organization',
              name: author,
            }
          : organization,
        publisher: organization,
        articleSection: section,
        inLanguage: 'en',
      });
    }

    if (breadcrumbs.length > 0) {
      graph.push({
        '@type': 'BreadcrumbList',
        '@id': `${canonicalUrl}#breadcrumb`,
        itemListElement: breadcrumbs.map((breadcrumb, index) => ({
          '@type': 'ListItem',
          position: index + 1,
          name: breadcrumb.name,
          item: new URL(breadcrumb.path, SITE_URL).href,
        })),
      });
    }

    if (graph.length === 0) return null;
    return JSON.stringify({ '@context': 'https://schema.org', '@graph': graph })
      .replace(/</g, '\\u003c')
      .replace(/>/g, '\\u003e')
      .replace(/&/g, '\\u0026')
      .replace(/\u2028/g, '\\u2028')
      .replace(/\u2029/g, '\\u2029');
  });
</script>

<svelte:head>
  <title>{title}</title>
  <meta name="description" content={description} />
  <link rel="canonical" href={canonicalUrl} />

  <meta property="og:site_name" content="Nogon" />
  <meta property="og:locale" content="en_US" />
  <meta property="og:title" content={title} />
  <meta property="og:description" content={description} />
  <meta property="og:type" content={type} />
  <meta property="og:url" content={canonicalUrl} />

  <meta name="twitter:card" content={twitterCard} />
  <meta name="twitter:title" content={title} />
  <meta name="twitter:description" content={description} />

  {#if imageUrl}
    <meta property="og:image" content={imageUrl} />
    <meta property="og:image:alt" content={imageAlt} />
    <meta name="twitter:image" content={imageUrl} />
    <meta name="twitter:image:alt" content={imageAlt} />
  {/if}

  {#if type === 'article'}
    {#if publishedTime}
      <meta property="article:published_time" content={publishedTime} />
    {/if}
    {#if modifiedTime}
      <meta property="article:modified_time" content={modifiedTime} />
    {/if}
    {#if author}
      <meta property="article:author" content={author} />
    {/if}
    {#if section}
      <meta property="article:section" content={section} />
    {/if}
  {/if}

  {#if structuredData}
    {@html `<script type="application/ld+json">${structuredData}</script>`}
  {/if}
</svelte:head>
