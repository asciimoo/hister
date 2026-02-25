---
date: '2026-02-25T00:00:00+00:00'
draft: false
title: 'Configuration Reference'
---

Hister is configured via a YAML file. The default location is:

- **Linux**: `~/.config/hister/config.yml` (or `$XDG_DATA_HOME/hister/config.yml`)
- **macOS**: `~/Library/Application Support/hister/config.yml`
- **Windows**: `%LOCALAPPDATA%\hister\config.yml`

Generate a config file with default values:

```bash
hister create-config ~/.config/hister/config.yml
```

**Important**: Restart the Hister server after modifying the configuration file.

## Full Example

```yaml
app:
  directory: "~/.config/hister"
  search_url: "https://google.com/search?q={query}"
  log_level: "info"
  open_results_on_new_tab: false

server:
  address: "127.0.0.1:4433"
  base_url: "http://127.0.0.1:4433"
  database: "db.sqlite3"

hotkeys:
  web:
    "/": "focus_search_input"
    "enter": "open_result"
    "alt+enter": "open_result_in_new_tab"
    "alt+j": "select_next_result"
    "alt+k": "select_previous_result"
    "alt+o": "open_query_in_search_engine"
    "alt+v": "view_result_popup"
    "tab": "autocomplete"
    "?": "show_hotkeys"

sensitive_content_patterns:
  aws_access_key: 'AKIA[0-9A-Z]{16}'
  github_token: '(ghp|gho|ghu|ghs|ghr)_[a-zA-Z0-9]{36}'
```

## TUI Configuration

TUI-specific settings are stored in a separate `tui.yaml` file in the same directory as your main config. This file is automatically created with defaults the first time you run `hister search`.

**Default location**: `~/.config/hister/tui.yaml` (or alongside your config file)

### tui.yaml Example

```yaml
dark_theme: "catppuccin-mocha"
light_theme: "catppuccin-latte"
color_scheme: "auto"

hotkeys:
  ctrl+c: "quit"
  f1: "toggle_help"
  tab: "toggle_focus"
  esc: "toggle_focus"
  up: "scroll_up"
  k: "scroll_up"
  down: "scroll_down"
  j: "scroll_down"
  enter: "open_result"
  ctrl+d: "delete_result"
  d: "delete_result"
  ctrl+t: "toggle_theme"
  t: "toggle_theme"
  ctrl+s: "toggle_settings"
  s: "toggle_settings"
  ctrl+o: "toggle_sort"
  o: "toggle_sort"
  alt+1: "tab_search"
  alt+2: "tab_history"
  alt+3: "tab_rules"
  alt+4: "tab_add"
```

---

## `app` Section

| Key                       | Type   | Default                               | Description                                                                   |
|---------------------------|--------|---------------------------------------|-------------------------------------------------------------------------------|
| `directory`               | string | platform default                      | Directory where Hister stores its data (index, rules, secret key).            |
| `search_url`              | string | `https://google.com/search?q={query}` | Fallback web search URL. Use `{query}` as the placeholder for the search term.|
| `log_level`               | string | `info`                                | Log verbosity. One of: `debug`, `info`, `warn`, `error`.                      |
| `debug_sql`               | bool   | `false`                               | Enable verbose SQL query logging.                                             |
| `open_results_on_new_tab` | bool   | `false`                               | Open search results in a new browser tab instead of the current tab.          |

---

## `server` Section

| Key        | Type   | Default                 | Description                                                                                                   |
|------------|--------|-------------------------|---------------------------------------------------------------------------------------------------------------|
| `address`  | string | `127.0.0.1:4433`        | Host and port to listen on. Use `0.0.0.0:4433` to listen on all interfaces.                                   |
| `base_url` | string | derived from `address`  | Public URL of the Hister instance. Required when `address` uses `0.0.0.0`. Must match how you access Hister.  |
| `database` | string | `db.sqlite3`            | SQLite database filename (relative to `app.directory`).                                                       |

---

## TUI Settings

TUI settings are configured in a separate `tui.yaml` file located in the same directory as your main config file. This file is automatically created with default values when you first run `hister search`.

### Theme Settings

| Key           | Type   | Default              | Description                                                                                             |
|---------------|--------|----------------------|---------------------------------------------------------------------------------------------------------|
| `dark_theme`  | string | `catppuccin-mocha`   | Theme to use in dark mode. Available themes: catppuccin, dracula, gruvbox, nord, rose-pine, tokyonight. |
| `light_theme` | string | `catppuccin-latte`   | Theme to use in light mode.                                                                             |
| `color_scheme`| string | `auto`               | Color scheme mode: `auto` (follow system), `dark`, or `light`.                                          |
| `themes_dir`  | string | (built-in themes)    | Custom directory for theme YAML files (optional).                                                       |

**Built-in themes**: catppuccin-mocha, catppuccin-frappe, catppuccin-macchiato, catppuccin-latte, dracula, gruvbox, nord, rose-pine, tokyonight.

---

## `hotkeys.web` Section

Defines keyboard shortcuts for the web interface. Each entry maps a key combination to an action.

**Key format**: `[modifier+]key` where modifier is `ctrl`, `alt`, or `meta`. Key can be a letter, digit, or special key (`enter`, `tab`, `arrowup`, `arrowdown`, etc.).

| Action                         | Description                                                     |
|--------------------------------|-----------------------------------------------------------------|
| `focus_search_input`           | Move focus to the search input box                              |
| `open_result`                  | Open the selected (or first) result                             |
| `open_result_in_new_tab`       | Open the selected result in a new tab                           |
| `select_next_result`           | Move selection to the next result                               |
| `select_previous_result`       | Move selection to the previous result                           |
| `open_query_in_search_engine`  | Open the current query in the configured fallback search engine |
| `view_result_popup`            | Open the offline preview popup for the selected result          |
| `autocomplete`                 | Accept the autocomplete suggestion                              |
| `show_hotkeys`                 | Show the keyboard shortcuts help overlay                        |

---

## TUI Hotkeys

TUI keyboard shortcuts are configured in `tui.yaml` under the `hotkeys` section. See the [tui.yaml example](#tui-configuration) above.

| Action            | Description                                                      |
|-------------------|------------------------------------------------------------------|
| `quit`            | Exit the TUI                                                     |
| `toggle_help`     | Show/hide the keybindings help overlay                           |
| `toggle_focus`    | Switch between search input and results list                     |
| `scroll_up`       | Move selection up                                                |
| `scroll_down`     | Move selection down                                              |
| `open_result`     | Open the selected result in your browser                         |
| `delete_result`   | Delete the selected entry from the index                         |
| `toggle_theme`    | Open the interactive theme picker overlay                        |
| `toggle_settings` | Open the keybinding editor overlay                               |
| `toggle_sort`     | Toggle domain-based sorting for search results                   |
| `tab_search`      | Switch to the Search tab                                         |
| `tab_history`     | Switch to the History tab (view recent searches)                 |
| `tab_rules`       | Switch to the Rules tab (manage blacklist/priority/alias rules)  |
| `tab_add`         | Switch to the Add tab (manually add URLs to index)               |

---

## `sensitive_content_patterns` Section

A map of named regex patterns. Any indexed page containing a match will have that field redacted before storage. Useful for preventing accidental indexing of secrets, tokens, or credentials.

```yaml
sensitive_content_patterns:
  my_pattern: 'regex here'
```

Default patterns cover common secrets: AWS keys, GitHub tokens, SSH/PGP private keys.

---

## Environment Variables

You can override configuration values using environment variables. The naming convention is:

```textplain
HISTER__<SECTION>__<KEY>=value
```

For example:

- `HISTER__APP__LOG_LEVEL=debug` overrides `app.log_level`
- `HISTER__SERVER__ADDRESS=0.0.0.0:8080` overrides `server.address`

Two special-purpose variables are also supported:

| Variable          | Description                                                           |
|-------------------|-----------------------------------------------------------------------|
| `HISTER_PORT`     | Override the port only (keeps the existing host from `server.address`)|
| `HISTER_DATA_DIR` | Override `app.directory`                                              |
