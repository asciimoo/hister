<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script module lang="ts">
  import type { HighlighterCore, ThemedToken } from 'shiki/core';

  export interface CodePreviewData {
    text: string;
    syntax?: string;
  }

  export function parseCodePreview(content: string): CodePreviewData {
    const data: unknown = JSON.parse(content);
    if (
      typeof data !== 'object' ||
      data === null ||
      Array.isArray(data) ||
      !('text' in data) ||
      typeof data.text !== 'string' ||
      ('syntax' in data && typeof data.syntax !== 'string')
    )
      throw new Error('Invalid code preview');
    return data as CodePreviewData;
  }

  const syntaxes = [
    { id: 'c', label: 'C', load: () => import('shiki/langs/c.mjs') },
    { id: 'cpp', label: 'C++', load: () => import('shiki/langs/cpp.mjs') },
    { id: 'csharp', label: 'C#', load: () => import('shiki/langs/csharp.mjs') },
    { id: 'css', label: 'CSS', load: () => import('shiki/langs/css.mjs') },
    { id: 'diff', label: 'Diff', load: () => import('shiki/langs/diff.mjs') },
    { id: 'dockerfile', label: 'Dockerfile', load: () => import('shiki/langs/dockerfile.mjs') },
    { id: 'go', label: 'Go', load: () => import('shiki/langs/go.mjs') },
    { id: 'html', label: 'HTML', load: () => import('shiki/langs/html.mjs') },
    { id: 'ini', label: 'INI', load: () => import('shiki/langs/ini.mjs') },
    { id: 'java', label: 'Java', load: () => import('shiki/langs/java.mjs') },
    { id: 'javascript', label: 'JavaScript', load: () => import('shiki/langs/javascript.mjs') },
    { id: 'json', label: 'JSON', load: () => import('shiki/langs/json.mjs') },
    { id: 'jsonc', label: 'JSON with comments', load: () => import('shiki/langs/jsonc.mjs') },
    { id: 'jsx', label: 'JSX', load: () => import('shiki/langs/jsx.mjs') },
    { id: 'lua', label: 'Lua', load: () => import('shiki/langs/lua.mjs') },
    { id: 'makefile', label: 'Makefile', load: () => import('shiki/langs/makefile.mjs') },
    { id: 'markdown', label: 'Markdown', load: () => import('shiki/langs/markdown.mjs') },
    { id: 'nix', label: 'Nix', load: () => import('shiki/langs/nix.mjs') },
    { id: 'php', label: 'PHP', load: () => import('shiki/langs/php.mjs') },
    { id: 'python', label: 'Python', load: () => import('shiki/langs/python.mjs') },
    { id: 'ruby', label: 'Ruby', load: () => import('shiki/langs/ruby.mjs') },
    { id: 'rust', label: 'Rust', load: () => import('shiki/langs/rust.mjs') },
    { id: 'scss', label: 'SCSS', load: () => import('shiki/langs/scss.mjs') },
    { id: 'shellscript', label: 'Shell', load: () => import('shiki/langs/shellscript.mjs') },
    { id: 'sql', label: 'SQL', load: () => import('shiki/langs/sql.mjs') },
    { id: 'toml', label: 'TOML', load: () => import('shiki/langs/toml.mjs') },
    { id: 'tsx', label: 'TSX', load: () => import('shiki/langs/tsx.mjs') },
    { id: 'typescript', label: 'TypeScript', load: () => import('shiki/langs/typescript.mjs') },
    { id: 'xml', label: 'XML', load: () => import('shiki/langs/xml.mjs') },
    { id: 'yaml', label: 'YAML', load: () => import('shiki/langs/yaml.mjs') },
  ];

  let highlighter: Promise<HighlighterCore> | undefined;
  const maxHighlightChars = 128 * 1024;
  const maxCodeLines = 5000;
  const maxLineLength = 10000;

  function getHighlighter() {
    return (highlighter ??= Promise.all([import('shiki/core'), import('shiki/engine/javascript')])
      .then(([{ createHighlighterCore }, { createJavaScriptRegexEngine }]) =>
        createHighlighterCore({
          themes: [import('shiki/themes/github-light.mjs'), import('shiki/themes/github-dark.mjs')],
          langs: [],
          engine: createJavaScriptRegexEngine(),
        }),
      )
      .catch((error) => {
        highlighter = undefined;
        throw error;
      }));
  }

  async function highlight(text: string, syntax: (typeof syntaxes)[number]) {
    const engine = await getHighlighter();
    await engine.loadLanguage(await syntax.load());
    // Normalize only the tokenization input. Rendering restores the original line endings.
    return engine.codeToTokens(text.replace(/\r\n|\r/g, '\n'), {
      lang: syntax.id,
      themes: { light: 'github-light', dark: 'github-dark' },
      tokenizeTimeLimit: 50,
    }).tokens;
  }
</script>

<script lang="ts">
  let { data, documentKey }: { data: CodePreviewData; documentKey: string } = $props();

  function readPreference(key: string, fallback: string): string {
    try {
      return localStorage.getItem(key) ?? fallback;
    } catch {
      return fallback;
    }
  }

  function savePreference(key: string, value: string) {
    try {
      if (value === 'auto') localStorage.removeItem(key);
      else localStorage.setItem(key, value);
    } catch {
      /* The preview also works when browser storage is unavailable. */
    }
  }

  let selected = $state('auto');
  let enabled = $state(readPreference('hister-code-highlight', 'true') !== 'false');
  let wrapLines = $state(readPreference('hister-code-wrap', 'true') !== 'false');
  let lineNumbers = $state(readPreference('hister-code-numbers', 'false') === 'true');
  let tokens = $state<ThemedToken[][] | null>(null);
  let failed = $state(false);
  const languageKey = $derived(`hister-code-language:${documentKey}`);
  const detected = $derived(syntaxes.find((syntax) => syntax.id === data.syntax));
  const active = $derived(
    selected === 'auto' ? detected : syntaxes.find((syntax) => syntax.id === selected),
  );
  const lines = $derived(
    data.text.length <= maxHighlightChars ? data.text.split(/\r\n|\r|\n/, maxCodeLines + 1) : [],
  );
  const large = $derived(
    data.text.length > maxHighlightChars ||
      lines.length > maxCodeLines ||
      lines.some((line) => line.length > maxLineLength),
  );
  const endings = $derived(large ? [] : (data.text.match(/\r\n|\r|\n/g) ?? []));

  $effect(() => {
    const saved = readPreference(languageKey, 'auto');
    selected =
      saved === 'plaintext' || syntaxes.some((syntax) => syntax.id === saved) ? saved : 'auto';
  });

  $effect(() => {
    const text = data.text;
    const syntax = active;
    tokens = null;
    failed = false;
    let cancelled = false;
    if (syntax && enabled && !large) {
      highlight(text, syntax)
        .then((result) => {
          if (!cancelled) tokens = result;
        })
        .catch(() => {
          if (!cancelled) failed = true;
        });
    }
    return () => {
      cancelled = true;
    };
  });
</script>

<section aria-label="Code preview" class="not-prose min-w-0 space-y-3">
  <label class="text-text-brand-secondary flex flex-wrap items-center gap-2 text-xs">
    Code language
    <select
      aria-label="Code language"
      bind:value={selected}
      disabled={large}
      onchange={() => savePreference(languageKey, selected)}
      class="bg-card-surface border-border-brand-muted min-w-0 border px-2 py-1"
    >
      <option value="auto">Automatic ({detected?.label ?? 'Plain text'})</option>
      <option value="plaintext">Plain text</option>
      {#each syntaxes as syntax}
        <option value={syntax.id}>{syntax.label}</option>
      {/each}
    </select>
  </label>
  <div class="text-text-brand-secondary flex flex-wrap gap-x-4 gap-y-2 text-xs">
    <label class="flex items-center gap-1.5"
      ><input
        type="checkbox"
        bind:checked={enabled}
        disabled={large}
        onchange={() => savePreference('hister-code-highlight', String(enabled))}
      /> Highlight syntax</label
    >
    <label class="flex items-center gap-1.5"
      ><input
        type="checkbox"
        bind:checked={wrapLines}
        onchange={() => savePreference('hister-code-wrap', String(wrapLines))}
      /> Wrap lines</label
    >
    <label class="flex items-center gap-1.5"
      ><input
        type="checkbox"
        bind:checked={lineNumbers}
        disabled={large}
        onchange={() => savePreference('hister-code-numbers', String(lineNumbers))}
      /> Line numbers</label
    >
  </div>
  {#if large}<p role="status" class="text-text-brand-muted text-xs">
      Large file. Showing plain text.
    </p>
  {:else if failed}<p role="status" class="text-text-brand-muted text-xs">
      Highlighting unavailable. Showing plain text.
    </p>{/if}
  <pre
    class="bg-card-surface-muted text-text-brand font-fira m-0 max-w-full overflow-x-auto p-3 text-xs leading-6"
    class:no-wrap={!wrapLines}
    class:numbered={lineNumbers && !large}
    style:--gutter-width={`${String(lines.length).length + 2}ch`}><code
      >{#if !large && (tokens || lineNumbers)}{#each lines as line, index}<span
            class="code-line"
            data-line-number={lineNumbers ? index + 1 : undefined}
            >{#if tokens}{#each tokens[index] as token}<span
                  class="code-token"
                  style:--code-light={token.htmlStyle?.color}
                  style:--code-dark={token.htmlStyle?.['--shiki-dark']}>{token.content}</span
                >{/each}{:else}{line}{/if}{endings[index] ?? ''}</span
          >{/each}{:else}{data.text}{/if}</code
    ></pre>
</section>

<style>
  pre {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    tab-size: 4;
  }
  pre.no-wrap {
    white-space: pre;
    overflow-wrap: normal;
  }
  code {
    display: block;
    padding: 0;
    background: transparent;
    font: inherit;
  }
  .no-wrap code {
    width: max-content;
    min-width: 100%;
  }
  .code-line {
    display: block;
    position: relative;
    min-height: 1.5rem;
  }
  .numbered .code-line {
    padding-left: var(--gutter-width);
  }
  .numbered .code-line::before {
    content: attr(data-line-number);
    position: absolute;
    left: 0;
    width: calc(var(--gutter-width) - 1ch);
    text-align: right;
    user-select: none;
    color: var(--color-text-brand-muted);
  }
  .code-token {
    color: var(--code-light);
  }
  :global(.dark) .code-token {
    color: var(--code-dark);
  }
</style>
