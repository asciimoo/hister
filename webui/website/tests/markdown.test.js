// SPDX-License-Identifier: AGPL-3.0-or-later
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import test from 'node:test';
import { compile, preprocess } from 'svelte/compiler';
import { resolveConfig } from 'vite';

const config = await resolveConfig({ logLevel: 'silent' }, 'serve');
const preprocessors = config.plugins.find((plugin) => plugin.name === 'vite-plugin-svelte:config')
  .api.options.preprocess;
const filename = resolve('src/content/docs/import.md');
const table = '| Source | Details |\n| --- | --- |\n| Browser | Saved pages |\n';

for (const [name, markdown] of [
  ['a table', table],
  [
    'a table inside a blockquote',
    table
      .split('\n')
      .map((line) => `> ${line}`)
      .join('\n'),
  ],
  ['the import guide', readFileSync(filename, 'utf8')],
]) {
  test(`${name} compiles without noninteractive tabindex warnings`, async () => {
    const { code } = await preprocess(markdown, preprocessors, { filename });
    const { warnings } = compile(code, { filename });
    assert.deepEqual(
      warnings
        .filter((warning) => warning.code === 'a11y_no_noninteractive_tabindex')
        .map(({ code, message }) => ({ code, message })),
      [],
    );
    assert.match(
      code,
      /<\w+\b(?=[^>]*\btabindex="0")(?=[^>]*\brole="region")(?=[^>]*\baria-label="Scrollable table")[^>]*>/,
      'Tables must remain named, keyboard focusable scroll regions',
    );
  });
}

test('the table exception does not hide unrelated accessibility warnings', async () => {
  const source = `<div tabindex="0">Before the table</div>\n\n${table}\n\n<div tabindex="0">After the table</div>`;
  const { code } = await preprocess(source, preprocessors, { filename });
  const { warnings } = compile(code, { filename });
  assert.equal(
    warnings.filter((warning) => warning.code === 'a11y_no_noninteractive_tabindex').length,
    2,
  );
});
