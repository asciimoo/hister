# Hister — Claude Code Instructions

## Projekt-Überblick

Hister ist ein lokaler Web-History-Suchserver in Go. Er indexiert Browser-History und macht sie per Volltext-Suche (Bleve) durchsuchbar.

## Repository

- **Upstream:** https://github.com/asciimoo/hister
- **Fork:** https://github.com/doobidoo/hister
- **Main branch:** `master`

## Remotes

```
origin → https://github.com/asciimoo/hister  (upstream, kein Push-Zugriff)
fork   → https://github.com/doobidoo/hister  (eigener Fork, Push möglich)
```

## ⚠️ Branch-Regeln (kritisch)

**IMMER von `origin/master` (upstream) branchen — NICHT von `fork/master`!**

```bash
# ✅ Korrekt
git checkout origin/master -b feat/mein-feature

# ❌ Falsch — fork/master kann voraus sein und zieht fremde Commits rein
git checkout master -b feat/mein-feature
```

**Vor jedem PR erstellen prüfen:**
```bash
git diff origin/master...HEAD --name-only
# Nur die eigenen Dateien sollten auftauchen
```

**Hintergrund:** Der Fork-master kann Commits enthalten, die noch nicht im upstream sind. Diese landen sonst ungewollt im PR-Diff und verärgern Maintainer.

## Lokaler Build & Install

```bash
# Build testen
go build ./...

# Installieren (nach Änderungen immer ausführen)
go install .
# Binary: /Users/hkr/go/bin/hister

# Nach go install: Claude Desktop neu starten (MCP Server reload)
```

## Aktuelle Branches & PRs

| PR | Branch | Inhalt | Status |
|----|--------|--------|--------|
| #77 | `feat/browser-auto-import` | Multi-Platform Browser Import + manueller BROWSER_TYPE DB_PATH | offen |
| #84 | `docs/mcp-integration-clean` | README + Hugo MCP-Doku | offen |
| #85 | `feat/mcp-server-clean` | MCP stdio-Server (HTTP API) | offen |
| #91 | `feat/import-ux` | --min-visit default=5, Dashboard-Stats | offen |
| #92 | `feat/skip-dead-links` | Private/lokale URLs überspringen (konfigurierbar) | offen |

## Architektur

```
hister.go                  # CLI-Einstiegspunkt (Cobra commands)
mcp.go                     # MCP stdio-Server (hister mcp)
config/config.go           # Konfiguration (App, Server, Import, Hotkeys)
server/
  server.go                # HTTP-Server & Routen
  api.go                   # Deklaratives Endpoint-Registry (neu in upstream)
  indexer/indexer.go       # Bleve Volltext-Index
  model/history.go         # SQLite Datenbank (GORM)
```

## Präferenzen des Repo Owners

Diese Punkte hat der Maintainer (asciimoo) explizit als Anforderungen kommuniziert — immer berücksichtigen:

### 1. MCP Server muss HTTP API nutzen (nicht direkt auf DB/Index zugreifen)

> "It would be better for the MCP server to use the HTTP API of Hister instead of calling directly the indexer."

- MCP-Handler dürfen **nie** direkt `indexer.*` oder `model.*` aufrufen
- Stattdessen immer HTTP-Requests an den laufenden Hister-Server
- CSRF-Bypass für CLI/MCP: `Origin: hister://` Header setzen

```go
// ✅ Korrekt
req.Header.Set("Origin", "hister://")
resp, err := client.Do(req)

// ❌ Falsch
results, err := indexer.Search(cfg, query)
```

### 2. Browser-Import: Pfade als Liste, manuelle Angabe erhalten

> "Perhaps the Path member of the browserDB object could be a list with all the possible common locations on mac/win/linux. Also, I'd keep the ability to optionally specify a browser type + path."

- `browserDB.paths []string` — geordnete Kandidaten, erster existierender gewinnt
- `browserDB.globs []string` — für Profil-basierte Verzeichnisse (z.B. Firefox)
- Plattform-spezifisch via `runtime.GOOS` (darwin/linux/windows)
- Manuelle Angabe immer möglich: `hister import chrome /pfad/zur/History`

### 3. Konfigurierbarkeit vor Hardcoding

> "What about making it configurable?" (zu skip_private_urls)

- Verhalten das Nutzer sinnvoll variieren könnten → immer in `config.yml` exponieren
- Sinnvolle Defaults setzen, aber opt-out ermöglichen
- Beispiel: `import.skip_private_urls: true` — default an, aber abschaltbar

### 4. Neue Features: Separate PRs

Features die unterschiedliche Themen abdecken immer in eigene Branches/PRs aufteilen. Nie mehrere unzusammenhängende Features in einem PR.

## MCP Integration

Der `hister mcp` Subcommand startet einen MCP stdio-Server mit 5 Tools.
**Wichtig:** Alle Handler nutzen die HTTP API — kein direkter DB/Index-Zugriff.

| Tool | HTTP Endpoint | Methode |
|------|--------------|---------|
| `search` | `/search?q=...` | GET |
| `index` | `/add` (via `indexURL()`) | POST |
| `delete` | `/delete` | POST |
| `list_recent` | `/api/history?kind=recent&limit=N` | GET |
| `top_visited` | `/api/history?kind=top&limit=N` | GET |

**SDK:** `github.com/mark3labs/mcp-go v0.44.0`

**CSRF-Bypass:** Alle MCP/CLI-Requests setzen `Origin: hister://`

### Claude Code (lokal)
```bash
claude mcp add hister /Users/hkr/go/bin/hister mcp
```

### Claude Desktop
`~/Library/Application Support/Claude/claude_desktop_config.json`:
```json
"hister": {
  "command": "/Users/hkr/go/bin/hister",
  "args": ["mcp"]
}
```

## Workflow: Neues Feature / PR

1. Von `origin/master` branchen: `git checkout origin/master -b feat/name`
2. Code anpassen + `go build ./...` — auf Fehler prüfen
3. `go install .` — Binary aktualisieren
4. Bei MCP-Änderungen: Claude Desktop neu starten
5. `git add`, `git commit`, `git push fork <branch>`
6. PR gegen `asciimoo/hister` erstellen
7. `git diff origin/master...HEAD --name-only` — nur eigene Dateien prüfen

## Key APIs

```go
// Suche
indexer.Search(cfg, &indexer.Query{Text: q, Limit: n}) (*Results, error)

// Indexieren (HTTP-Download + Indexieren via POST /add)
indexURL(u string, skipPrivate bool) error

// Löschen
indexer.Delete(url string) error

// Anzahl indexierter Seiten
indexer.Count() (uint64, error)

// History
model.GetLatestHistoryItems(limit int) ([]*HistoryItem, error)
model.GetURLsByQuery(q string) ([]*URLCount, error)  // "" = alle, nach Count DESC
```

## Config-Schema (relevante Additions)

```yaml
import:
  skip_private_urls: true   # false = localhost/LAN-URLs auch indexieren

# Import CLI flags:
#   --min-visit N          nur URLs mit >= N Besuchen (default: 5)
#   --include-private      überschreibt skip_private_urls für diesen Lauf
```

## MCP Test (lokal)

```python
import subprocess, json, threading

proc = subprocess.Popen(['/Users/hkr/go/bin/hister', 'mcp'],
    stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, bufsize=0)

# Korrekte Reihenfolge:
# 1. initialize
# 2. notifications/initialized  (keine Antwort erwartet)
# 3. tools/list / tools/call
```

**Pitfall:** Alte hister-Prozesse blockieren SQLite → `pkill -f hister-test` vor Tests.
