# Hister — Claude Code Instructions

## Projekt-Überblick

Hister ist ein lokaler Web-History-Suchserver in Go. Er indexiert Browser-History und macht sie per Volltext-Suche (Bleve) durchsuchbar.

## Repository

- **Upstream:** https://github.com/asciimoo/hister
- **Fork:** https://github.com/doobidoo/hister
- **Main branch:** `master`

## Lokaler Build & Install

```bash
# Build
go build ./...

# Installieren (nach Änderungen immer ausführen)
go install .
# Binary landet in: /Users/hkr/go/bin/hister

# Nach go install: Claude Desktop neu starten, damit der MCP Server neu geladen wird
```

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

### Claude Code (lokal, dieses Repo)

Bereits konfiguriert via:
```bash
claude mcp add hister /Users/hkr/go/bin/hister mcp
```

### Claude Desktop

Konfiguriert in `~/Library/Application Support/Claude/claude_desktop_config.json`:
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
5. Commit & Push zum Fork
6. PR gegen `asciimoo/hister` (Branch: `feat/mcp-server`)

## Offene PRs

- **PR #76** — `fix/case-insensitive-search`: Groß-/Kleinschreibung bei Suche
- **PR #78** — `feat/mcp-server`: MCP Integration

## Key APIs

```go
// Suche
indexer.Search(cfg, &indexer.Query{Text: q, Limit: n}) (*Results, error)

// Indexieren (macht HTTP-Download + POST /add)
indexURL(url string) error

// Löschen
indexer.Delete(url string) error

// History
model.GetLatestHistoryItems(limit int) ([]*HistoryItem, error)
model.GetURLsByQuery(q string) ([]*URLCount, error)  // "" = alle, nach Count DESC
```
