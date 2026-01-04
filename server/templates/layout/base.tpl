{{ define "base" -}}
<!doctype html>
<html lang='en'>
    <head>
        <meta charset='utf-8'>
        <title>Hister</title>
		<link rel="stylesheet" type="text/css" href="/static/style.css" />
          <link href="/favicon.ico" rel="icon shortcut" type="image/x-icon" />
    </head>
    <body>
        <header>
            <h1 class="menu-item"><a href='/'>Hister</a></h1
            ><a class="menu-item" href="/rules">Rules</a
            ><a class="menu-item" href="/add">Add</a>
        </header>
        <main>
            {{ template "main" . }}
        </main>
        <footer>
            <a href='https://github.com/asciimoo/hister/'>GitHub</a>
        </footer>
    </body>
</html>
{{- end -}}
