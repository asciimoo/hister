import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { test } from 'node:test';
import { buildCrawlCommand } from './crawl-command.js';

// Exercise actual POSIX shell parsing without invoking Hister or the network.
function argumentsFor(server, site, pages = 100, delay = 1) {
  const { command } = buildCrawlCommand(server, site, pages, delay);
  const result = spawnSync('/bin/sh', ['-c', `hister() { printf '%s\\0' "$@"; }; ${command}`], {
    encoding: 'utf8',
  });
  assert.equal(result.status, 0, result.stderr);
  return result.stdout.split('\0').slice(0, -1);
}

test('targets the remote server and starts from the site home page', () => {
  assert.deepEqual(
    argumentsFor('https://hister.example/base/', 'https://example.org/article?q=x#part'),
    [
      '--server-url',
      'https://hister.example/base/',
      'index',
      '--recursive',
      '--max-links',
      '100',
      '--delay',
      '1',
      '--allowed-pattern',
      '^https://example\\.org/',
      '--',
      'https://example.org/',
    ],
  );
});

test('quotes shell metacharacters as literal arguments', () => {
  const server = "https://hister.example/a'$(printf INJECTED)`printf INJECTED`";
  const args = argumentsFor(server, 'https://example.org/');
  assert.equal(args[1], new URL(server).href);
  assert.equal(args.length, 12);
});

test('link patterns exclude subdomains, lookalike hosts, and other ports', () => {
  for (const site of ['https://example.org/', 'http://example.org:8080/', 'http://[::1]:8080/']) {
    const args = argumentsFor('https://hister.example/', site, 42, 3);
    const pattern = new RegExp(args[9]);
    assert.ok(pattern.test(site + 'article'));
    assert.ok(!pattern.test('https://sub.example.org/'));
    assert.ok(!pattern.test('https://exampleXorg/'));
    assert.ok(!pattern.test('https://example.org.evil.test/'));
    assert.ok(!pattern.test('https://example.org:8443/'));
    assert.equal(args[5], '42');
    assert.equal(args[7], '3');
  }
});

test('rejects unsupported URLs and embedded credentials', () => {
  for (const site of [
    '',
    'not a URL',
    'chrome://extensions',
    'file:///tmp/page',
    'https://user:secret@example.org/',
  ]) {
    assert.throws(() => buildCrawlCommand('https://hister.example/', site, 100, 1));
  }
  for (const server of [
    '',
    'ftp://example.org',
    'https://user:secret@example.org/',
    'https://example.org/?token=secret',
    'https://example.org/#secret',
  ]) {
    assert.throws(() => buildCrawlCommand(server, 'https://example.org/', 100, 1));
  }
});

test('rejects invalid limits and delays, including empty number inputs', () => {
  for (const pages of [undefined, NaN, 0, -1, 1.5, 1001, '100']) {
    assert.throws(() =>
      buildCrawlCommand('https://hister.example/', 'https://example.org/', pages, 1),
    );
  }
  for (const delay of [undefined, NaN, 0, -1, 1.5, 61, '1']) {
    assert.throws(() =>
      buildCrawlCommand('https://hister.example/', 'https://example.org/', 100, delay),
    );
  }
});
