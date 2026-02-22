{{ define "base" -}}
<!doctype html>
<html lang="en" data-theme="dark">
    <head>
        <meta charset="utf-8">
        <title>Hister</title>
        <link rel="stylesheet" type="text/css" href="/static/style.css" />
        <meta name="referrer" content="no-referrer">
		<link href="/opensearch.xml" rel="search" title="Hister" type="application/opensearchdescription+xml" data-testid="opensearch"/>
        <link href="data:image/x-icon;base64,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" rel="icon shortcut" type="image/x-icon" />
    </head>
    <body>
        <header>
            <h1 class="menu-item"><img src="/static/logo.png" /> <a href='/'>Hister</a></h1>
            <a class="menu-item" href="/history">History</a>
            <a class="menu-item" href="/rules">Rules</a>
            <a class="menu-item" href="/add">Add</a>
            <button id="theme-toggle" class="theme-toggle float-right" title="Toggle theme">
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <circle cx="12" cy="12" r="5"/>
                    <line x1="12" y1="1" x2="12" y2="3"/>
                    <line x1="12" y1="21" x2="12" y2="23"/>
                    <line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/>
                    <line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/>
                    <line x1="1" y1="12" x2="3" y2="12"/>
                    <line x1="21" y1="12" x2="23" y2="12"/>
                    <line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/>
                    <line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/>
                </svg>
            </button>
        </header>
        <main>
            {{ if .Success }}
            <div class="container box success">
                <div class="header">{{ .Success }}</div>
                {{ if .SuccessMsg }}<div class="content">{{ .SuccessMsg }}</div>{{ end }}
            </div>
            {{ end }}
            {{ template "main" . }}
        </main>
        <footer>
            <a href="/help">Help</a> |
            <a href="/about">About</a> |
            <a href="/api">API</a> |
            <a href="https://github.com/asciimoo/hister/">GitHub</a>
            <span class="footer-meta">
                {{ if .IndexCount }}{{ .IndexCount }} pages indexed | {{ end }}{{ .AppVersion }}
            </span>
        </footer>
        <script src="/static/js/site.js" nonce="{{ .Nonce }}"></script>
    </body>
</html>
{{- end -}}
