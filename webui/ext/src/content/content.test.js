import assert from 'node:assert/strict';
import { test } from 'node:test';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import { build } from 'vite';

// Exercise the actual bundled content script with deterministic browser events
// and time. No browser, network requests, or generated files are needed.
const bundle = await build({
  configFile: false,
  logLevel: 'silent',
  build: {
    write: false,
    minify: false,
    rolldownOptions: {
      input: fileURLToPath(new URL('./content.ts', import.meta.url)),
      output: { format: 'iife' },
    },
  },
});
const script = bundle.output.find((entry) => entry.type === 'chunk').code;

function browser({
  hidden = false,
  status = 200,
  contentType = 'text/html',
  respond = true,
  frame = false,
} = {}) {
  let now = 0;
  let nextTimer = 0;
  let onMessage;
  const timers = new Map();
  const listeners = new Map();
  const messages = [];
  const reads = { text: 0, html: 0 };
  const delays = [];
  const observers = [];
  const page = {
    url: 'https://example.com/article',
    title: 'Article',
    text: 'The article text.',
    html: '<head><title>Article</title></head><body>The article text.</body>',
    favicon: '/favicon.ico',
    metadata: [],
    editor: null,
  };
  const addEventListener = (name, fn) => {
    if (!listeners.has(name)) listeners.set(name, []);
    listeners.get(name).push(fn);
  };
  const document = {
    readyState: 'complete',
    contentType,
    hidden,
    addEventListener,
    body: {
      get innerText() {
        reads.text++;
        return page.text;
      },
    },
    documentElement: {
      get innerHTML() {
        reads.html++;
        return page.html;
      },
    },
    querySelector: (selector) => {
      if (selector === 'title') return { innerText: page.title };
      if (selector === '[data-lexical-editor="true"]') return page.editor;
      return { getAttribute: () => page.favicon };
    },
    querySelectorAll: () => page.metadata,
  };
  const window = {
    get location() {
      return new URL(page.url);
    },
    document,
    addEventListener,
    navigation: { addEventListener },
    performance: { getEntries: () => [{ entryType: 'navigation', responseStatus: status }] },
  };
  window.top = frame ? {} : window;
  if (frame) page.url = 'https://docs-editor.proton.me/?type=doc&mode=edit';
  const runtime = {
    id: 'extension',
    onMessage: { addListener: (fn) => (onMessage = fn) },
    sendMessage: (request, callback) => {
      messages.push({ request: structuredClone(request), callback, at: now });
      if (respond) callback?.({ status_code: 201 });
    },
  };
  vm.runInNewContext(script, {
    window,
    document,
    chrome: { runtime },
    URL,
    Date: { now: () => now },
    console,
    MutationObserver: class {
      constructor(fn) {
        observers.push(fn);
      }
      observe() {}
      disconnect() {}
    },
    setTimeout: (fn, delay) => {
      assert.ok(Number.isFinite(delay) && delay >= 0 && delay <= 300_000);
      delays.push(delay);
      const id = ++nextTimer;
      timers.set(id, { fn, due: now + delay });
      return id;
    },
    clearTimeout: (id) => timers.delete(id),
  });
  const emit = (name, event = {}) => listeners.get(name)?.forEach((fn) => fn(event));
  return {
    page,
    reads,
    delays,
    messages,
    timers,
    runtime,
    navigate() {
      emit('navigatesuccess');
    },
    hide() {
      document.hidden = true;
      emit('visibilitychange');
    },
    show() {
      document.hidden = false;
      emit('visibilitychange');
    },
    leave() {
      emit('pagehide');
    },
    close() {
      emit('pagehide');
      runtime.id = '';
      timers.clear();
    },
    restore() {
      document.hidden = false;
      emit('pageshow', { persisted: true });
    },
    mutate() {
      observers.forEach((fn) => fn([]));
    },
    embed(embeddedContent) {
      return onMessage({ embeddedContent }, {}, () => {});
    },
    reindex(callback = () => {}) {
      return onMessage({ action: 'reindex' }, {}, callback);
    },
    advance(ms) {
      const end = now + ms;
      let ticks = 0;
      while (timers.size) {
        const [id, timer] = [...timers].sort((a, b) => a[1].due - b[1].due)[0];
        if (timer.due > end) break;
        assert.ok(++ticks < 10_000, 'timer loop must remain bounded');
        timers.delete(id);
        now = timer.due;
        timer.fn();
      }
      now = end;
    },
  };
}

function metadata(tagName, attributes, textContent = '') {
  return { tagName, textContent, getAttribute: (name) => attributes[name] ?? null };
}

test('cosmetic HTML changes wait for the preview interval without repeated DOM serialization', () => {
  const b = browser();
  b.advance(0);
  for (let n = 1; n < 30; n++) {
    b.page.html = `<body class="state-${n}">The article text.</body>`;
    b.advance(10_000);
  }
  assert.equal(b.messages.length, 1);
  assert.equal(b.reads.html, 1);
  b.advance(10_000);
  assert.equal(b.messages.length, 2);
  assert.equal(b.messages[1].at, 300_000);
  assert.equal(b.messages[1].request.pageData.html, b.page.html);
});

test('navigation bursts defer capture and submit only the latest page after the minimum interval', () => {
  const b = browser();
  b.advance(0);
  for (let n = 0; n < 100; n++) {
    b.page.url = `https://example.com/page/${n}`;
    b.page.text = `Page ${n}`;
    b.navigate();
  }
  assert.equal(b.reads.text, 1);
  b.advance(29_999);
  assert.equal(b.messages.length, 1);
  b.advance(1);
  assert.equal(b.messages.length, 2);
  assert.equal(b.messages[1].request.pageData.url, b.page.url);
  assert.equal(b.messages[1].request.pageData.text, 'Page 99');
});

test('navigation settles for one second after the final event when the cooldown has elapsed', () => {
  const b = browser();
  b.advance(60_000);
  b.page.text = 'First navigation';
  b.navigate();
  b.advance(900);
  b.page.text = 'Settled navigation';
  b.navigate();
  b.advance(999);
  assert.equal(b.messages.length, 1);
  b.advance(1);
  assert.equal(b.messages[1].request.pageData.text, 'Settled navigation');
});

test('unchanged navigation events cannot repeatedly extract a large page', () => {
  const b = browser();
  b.advance(0);
  for (let n = 0; n < 100; n++) {
    b.navigate();
    b.advance(1500);
  }
  assert.equal(b.messages.length, 1);
  assert.ok(b.reads.text <= 6);
  assert.equal(b.reads.html, 1);
});

test('text, title, favicon, and structured metadata changes trigger updates with full HTML', () => {
  const changes = [
    (page) => (page.text = 'Updated article'),
    (page) => (page.title = 'Updated title'),
    (page) => (page.favicon = '/new.ico'),
    (page) => (page.metadata = [metadata('META', { property: 'og:description', content: 'New' })]),
    (page) => (page.metadata = [metadata('LINK', { href: 'https://example.com/canonical' })]),
    (page) => (page.metadata = [metadata('SCRIPT', {}, '{"@type":"Article"}')]),
  ];
  for (const change of changes) {
    const b = browser();
    b.advance(0);
    change(b.page);
    b.page.html = '<body>Latest preview</body>';
    b.advance(30_000);
    assert.equal(b.messages.length, 2);
    assert.equal(b.messages[1].request.pageData.html, b.page.html);
    assert.equal('metadata' in b.messages[1].request.pageData, false);
  }
});

test('hiding captures one final snapshot and sends it after cooldown without further extraction', () => {
  const b = browser();
  b.advance(1000);
  b.page.text = 'Final visible text';
  b.page.html = '<body>Final visible text</body>';
  b.hide();
  const reads = { ...b.reads };
  b.page.text = 'Background animation';
  b.navigate();
  b.advance(60 * 60 * 1000);
  assert.equal(b.messages.length, 2);
  assert.equal(b.messages[1].at, 30_000);
  assert.equal(b.messages[1].request.pageData.text, 'Final visible text');
  assert.deepEqual(b.reads, reads);
  assert.equal(b.timers.size, 0);
});

test('showing a tab replaces its pending hidden snapshot with fresh content', () => {
  const b = browser();
  b.advance(1000);
  b.page.text = 'Hidden snapshot';
  b.hide();
  b.advance(1000);
  b.page.text = 'Visible again';
  b.show();
  b.advance(29_000);
  assert.equal(b.messages.length, 2);
  assert.equal(b.messages[1].request.pageData.text, 'Visible again');
});

test('closing a tab immediately hands its pending final snapshot to the background', () => {
  const b = browser();
  b.advance(1000);
  b.page.text = 'Final text';
  b.page.html = '<body>Final text</body>';
  b.hide();
  assert.equal(b.messages.length, 1);
  b.close();
  assert.equal(b.messages.length, 2);
  assert.equal(b.messages[1].at, 1000);
  assert.equal(b.messages[1].request.pageData.text, 'Final text');
  assert.equal(b.messages[1].request.pageData.html, b.page.html);
  assert.equal(b.messages[1].request.action, undefined);
  assert.equal(b.timers.size, 0);
});

test('closing captures the latest state even without an earlier visibility event', () => {
  const b = browser();
  b.advance(1000);
  b.page.text = 'Latest text';
  b.close();
  assert.equal(b.messages.length, 2);
  assert.equal(b.messages[1].request.pageData.text, 'Latest text');
});

test('closing captures HTML changes and does not send unchanged content twice', () => {
  const b = browser();
  b.advance(1000);
  b.page.html = '<body class="updated">The article text.</body>';
  b.leave();
  b.hide();
  b.close();
  assert.equal(b.messages.length, 2);
  assert.equal(b.messages[1].request.pageData.html, b.page.html);
});

test('closing uses current content instead of an older pending snapshot', () => {
  const b = browser();
  b.advance(1000);
  b.page.text = 'Pending text';
  b.hide();
  b.page.text = 'Final text';
  b.close();
  assert.equal(b.messages.length, 2);
  assert.equal(b.messages[1].request.pageData.text, 'Final text');
});

test('closing does not submit a pending change that reverted to the previous version', () => {
  const b = browser();
  b.advance(1000);
  const original = b.page.text;
  b.page.text = 'Pending text';
  b.hide();
  b.page.text = original;
  b.close();
  assert.equal(b.messages.length, 1);
});

test('closing unchanged, skipped, or never viewed pages does not submit them', () => {
  const unchanged = browser();
  unchanged.advance(1000);
  unchanged.close();
  assert.equal(unchanged.messages.length, 1);

  const skipped = browser({ respond: false });
  skipped.advance(1000);
  skipped.page.text = 'Pending text';
  skipped.hide();
  skipped.messages[0].callback({ status_code: 406 });
  skipped.close();
  assert.equal(skipped.messages.length, 1);

  const hidden = browser({ hidden: true });
  hidden.close();
  assert.equal(hidden.messages.length, 0);
});

test('polling resumes when a page is restored from the browser history cache', () => {
  const b = browser();
  b.advance(1000);
  b.leave();
  b.advance(60_000);
  assert.equal(b.timers.size, 0);
  b.page.text = 'Restored content';
  b.restore();
  b.advance(1000);
  assert.equal(b.messages.length, 2);
  assert.equal(b.messages[1].request.pageData.text, 'Restored content');
});

test('initially hidden pages defer extraction until visible', () => {
  const b = browser({ hidden: true });
  b.advance(60_000);
  assert.deepEqual(b.reads, { text: 0, html: 0 });
  b.show();
  b.advance(1000);
  assert.equal(b.messages.length, 1);
});

test('unchanged pages back off to a bounded interval and never resubmit', () => {
  const b = browser();
  b.advance(2 * 24 * 60 * 60 * 1000);
  assert.equal(b.messages.length, 1);
  assert.ok(b.delays.includes(300_000));
  assert.ok(b.reads.text < 600);
});

test('manual reindex bypasses cooldown and unchanged content, including while hidden', () => {
  const b = browser();
  b.advance(0);
  b.page.text = 'Pending change';
  b.hide();
  let response;
  assert.equal(
    b.reindex((value) => (response = value)),
    true,
  );
  assert.equal(response.status_code, 201);
  assert.equal(b.messages.length, 2);
  assert.equal(b.messages[1].request.action, 'reindex');
  assert.equal(b.messages[1].at, 0);
  b.advance(60_000);
  assert.equal(b.messages.length, 2);
  assert.equal(b.timers.size, 0);
});

test('skip rejection suppresses polling and a final snapshot, and manual reindex bypasses it', () => {
  const b = browser({ respond: false });
  b.advance(0);
  b.page.text = 'Changed before rejection';
  b.hide();
  b.messages[0].callback({ status_code: 406 });
  b.advance(30_000);
  assert.equal(b.messages.length, 1);
  b.show();
  b.advance(60_000);
  assert.equal(b.messages.length, 1);
  b.reindex();
  assert.equal(b.messages.length, 2);
  assert.equal(b.messages[1].request.action, 'reindex');
});

test('a delayed response cannot skip a newer submission', () => {
  const b = browser({ respond: false });
  b.advance(0);
  b.page.url = 'https://example.com/new';
  b.advance(30_000);
  b.messages[0].callback({ status_code: 406 });
  b.page.text = 'Latest';
  b.advance(30_000);
  assert.equal(b.messages.length, 3);
});

test('transient failures retry after the minimum interval without a new page change', () => {
  const b = browser({ respond: false });
  b.advance(0);
  b.messages[0].callback({ status_code: 500 });
  b.advance(29_999);
  assert.equal(b.messages.length, 1);
  b.advance(1);
  assert.equal(b.messages.length, 2);
});

test('permanent rejection does not repeatedly submit unchanged content', () => {
  const b = browser({ respond: false });
  b.advance(0);
  b.messages[0].callback({ status_code: 422 });
  b.advance(600_000);
  assert.equal(b.messages.length, 1);
});

test('unsupported content is never extracted, and manual indexing can override an HTTP error', () => {
  const unsupported = browser({ contentType: 'application/pdf' });
  unsupported.advance(60_000);
  unsupported.reindex();
  assert.deepEqual(unsupported.reads, { text: 0, html: 0 });
  const errorPage = browser({ status: 404 });
  errorPage.advance(60_000);
  assert.equal(errorPage.messages.length, 0);
  errorPage.reindex();
  assert.equal(errorPage.messages.length, 1);
});

test('embedded frame content is merged into the top frame submission', () => {
  const b = browser();
  b.advance(0);
  assert.equal(b.messages.length, 1);
  b.embed({ html: '<p>Doc body</p>', text: 'Doc body' });
  b.advance(30_000);
  assert.equal(b.messages.length, 2);
  const { pageData } = b.messages[1].request;
  assert.equal(pageData.text, 'The article text.\n\nDoc body');
  assert.equal(
    pageData.html,
    '<head><title>Article</title></head><body>The article text.<article><p>Doc body</p></article></body>',
  );
});

test('embedded content frame relays editor content and never indexes the iframe URL', () => {
  const b = browser({ frame: true });
  // The editor mounts after the script starts and is picked up by the observer
  assert.equal(b.messages.length, 0);
  b.page.editor = { innerHTML: '<p>Hello</p>', innerText: 'Hello' };
  b.mutate();
  b.advance(2_000);
  assert.deepEqual(
    b.messages.map((m) => m.request),
    [{ embeddedContent: { html: '<p>Hello</p>', text: 'Hello' } }],
  );
  // Unchanged content is not relayed again
  b.mutate();
  b.advance(2_000);
  assert.equal(b.messages.length, 1);
  // Edits are throttled into one relay
  b.page.editor = { innerHTML: '<p>Hello world</p>', innerText: 'Hello world' };
  b.mutate();
  b.mutate();
  b.advance(2_000);
  assert.equal(b.messages.length, 2);
  assert.equal(b.messages[1].request.embeddedContent.text, 'Hello world');
  // Reindex requests broadcast to the tab are left to the top frame
  assert.equal(b.reindex(), undefined);
  b.advance(600_000);
  assert.ok(b.messages.every((m) => !m.request.pageData));
});
