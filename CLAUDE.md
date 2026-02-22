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
| #77 | `feat/browser-auto-import` | Auto-detect Browser Import | offen |
| #84 | `docs/mcp-integration-clean` | README + Hugo MCP-Doku | offen |
| #85 | `feat/mcp-server-clean` | MCP stdio-Server | offen |

## Architektur

```
hister.go                  # CLI-Einstiegspunkt (Cobra commands)
mcp.go                     # MCP stdio-Server (hister mcp)
config/config.go           # Konfiguration
server/
  server.go                # HTTP-Server & Routen
  indexer/indexer.go       # Bleve Volltext-Index
  model/history.go         # SQLite Datenbank (GORM)
```

## MCP Integration

Der `hister mcp` Subcommand startet einen MCP stdio-Server mit 5 Tools:

| Tool | Funktion |
|------|----------|
| `search` | Volltext-Suche via `indexer.Search()` |
| `index` | URL herunterladen & indexieren via `indexURL()` |
| `delete` | URL aus Index löschen via `indexer.Delete()` |
| `list_recent` | Zuletzt besuchte Seiten via `model.GetLatestHistoryItems()` |
| `top_visited` | Meist besuchte Seiten via `model.GetURLsByQuery("")` |

**SDK:** `github.com/mark3labs/mcp-go v0.44.0`

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

## Workflow: Änderungen am MCP Server

1. Code in `mcp.go` anpassen
2. `go build ./...` — auf Fehler prüfen
3. `go install .` — Binary aktualisieren
4. Claude Desktop neu starten
5. `git add`, `git commit`, `git push fork <branch>`
6. PR gegen `asciimoo/hister` (Branch von `origin/master`)

## Key APIs

```go
// Suche
indexer.Search(cfg, &indexer.Query{Text: q, Limit: n}) (*Results, error)

// Indexieren (macht HTTP-Download + direktes Indexieren)
indexURL(url string) error

// Löschen
indexer.Delete(url string) error

// History
model.GetLatestHistoryItems(limit int) ([]*HistoryItem, error)
model.GetURLsByQuery(q string) ([]*URLCount, error)  // "" = alle, nach Count DESC
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
