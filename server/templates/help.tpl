{{define "main"}}
<div class="container">
<h2>Search Shortcuts</h2>
<p>Press <kbd>enter</kbd> to open the first result.</p>
<p>Navigate in results with <kbd>ctrl+j</kbd> and <kbd>ctrl+k</kbd>. <kbd>Enter</kbd> opens the selected result.</p>
<p>Press <kbd>ctrl+o</kbd> to open the search query in the configured search engine.</p>
<h2>Search Syntax</h2>
<p>Use <kbd>quotes</kbd> to match phrases.</p>
<p>Use <kbd>*</kbd> for wildcard matches.</p>
<p>Prefix words or phrases with <kbd>-</kbd> to exclude matching documents.</p>
<p>Use <code>url:</code> prefix to search only in the URL field.</p>
<h3>Examples</h3>
<p><code>"free software" url:*wikipedia.org*</code>: Search for the phrase "free software" only in URLs containing wikipedia.org.</p>
<p><code>golang template -url:*stackoverflow*</code>: Search sites containing both "golang" and "template" but the website should not contain "stackoverflow".</p>
<h2>Search Aliases</h2>
<p>Queries can become long and complex quickly. Aliases can be defined in the <a href="/rules">rules</a> page to shorten common query parts.</p>
<h3>Examples</h3>
<p><code>go</code> alias for <code>(go|golang)</code> matches either "go" or "golang" if you type "go".</p>
<p><code>!so</code> alias for <code>url:*stackoverflow.com*</code> matches only sites where the URL contain "stackoverflow.com".</p>
</div>
{{end}}
