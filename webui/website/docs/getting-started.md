---
date: '2026-02-13T10:59:19+01:00'
draft: false
title: 'Getting Started'
---

## Installation

### Option 1: Pre-built Binary

1. Download the binary for your platform:

- For stable versions: [Releases](https://github.com/asciimoo/hister/releases)
- For the latest development version: [Rolling Release (latest)](https://github.com/asciimoo/hister/releases/tag/rolling)

2. Make the binary executable:

```bash
chmod +x hister
```

3. Optionally, move it to your PATH:

```bash
sudo mv hister /usr/local/bin/
```

### Option 2: Build from Source

**Requirements**: Go 1.16 or later

1. Clone the repository:

   ```bash
   git clone https://github.com/asciimoo/hister.git
   cd hister
   ```

2. Build the binary:

   ```bash
   go build
   ```

3. The `hister` binary will be created in the current directory

### Option 3: [Nix](#nix)

### Option 4: [Docker](https://github.com/asciimoo/hister/blob/master/Dockerfile)

## Browser Extension Setup

To automatically index your browsing history, install the browser extension:

- **Chrome**: [Install from Chrome Web Store](https://chromewebstore.google.com/detail/hister/cciilamhchpmbdnniabclekddabkifhb)
- **Firefox**: [Install from Firefox Add-ons](https://addons.mozilla.org/en-US/firefox/addon/hister/)

After installing the extension, configure it to point to your Hister server (default: `http://127.0.0.1:4433`).

## First Run

Check available commands:

   ```bash
   ./hister help
   ```

1. Start the Hister server:

   ```bash
   ./hister listen
   ```

2. Open your browser and navigate to `http://127.0.0.1:4433`

3. You should see the Hister web interface

## Configuration

Hister can be configured using a YAML configuration file located at `~/.config/hister/config.yml`.

### Generate Default Configuration

To create a configuration file with default values:

```bash
./hister create-config ~/.config/hister/config.yml
```

**Important**: Restart the Hister server after modifying the configuration file.

## Importing Existing Browser History

You can import your existing browser history from Firefox or Chrome:

### Firefox

```bash
./hister import firefox [db path]
```

On linux DB path can be usually found at `/home/[USER]/.mozilla/[PROFILE]/places.sqlite`

### Chrome

```bash
./hister import chrome [db path]
```

On linux DB path can be usually found at `/home/[USER]/.config/chromium/Default/History`

## Command Line Usage

View all available commands:

```bash
./hister help
```

### Index a URL Manually

To manually index a specific URL:

```bash
./hister index https://example.com
```

## Using Hister

Once set up:

1. **Browse the web** with the extension installed - pages are automatically indexed
2. **Search your history** by visiting the Hister web interface
3. **Use advanced queries** with the [Bleve query syntax](https://blevesearch.com/docs/Query-String-Query/)
4. **Create keyword aliases** for frequently searched topics
5. **Configure blacklists** to exclude unwanted content

## TUI (Terminal UI)

Hister provides a terminal-based user interface for searching your browsing history without leaving your terminal.

### Start the TUI

Run the search command without any arguments:

```bash
hister search
```

### TUI Features

- **Multi-tab interface**: Search, History, Rules, and Add tabs
- **Mouse support**: Scroll with mouse wheel, click to select, right-click for context menu
- **Theming**: Built-in color themes with interactive picker (press `ctrl+t`)
- **Settings overlay**: Edit keybindings interactively (press `ctrl+s`)
- **Context menu**: Right-click on results for quick actions (open, delete, prioritize)

### Tabs

- **Search** (Alt+1): Main search interface
- **History** (Alt+2): View your recent search history
- **Rules** (Alt+3): Manage blacklist, priority, and alias rules
- **Add** (Alt+4): Manually add URLs to the index

### TUI Keybindings

The TUI uses the following keybindings by default:

| Key                | Action          | Description                                     |
|--------------------|-----------------|-------------------------------------------------|
| `ctrl+c`           | quit            | Exit the TUI                                    |
| `f1`               | toggle_help     | Show/hide keybindings help overlay              |
| `tab`, `esc`       | toggle_focus    | Switch between search input and results list    |
| `up`, `k`          | scroll_up       | Navigate up in results                          |
| `down`, `j`        | scroll_down     | Navigate down in results                        |
| `enter`            | open_result     | Open the selected result in your browser        |
| `ctrl+d`, `d`      | delete_result   | Delete the selected result from the index       |
| `ctrl+t`, `t`      | toggle_theme    | Open the interactive theme picker               |
| `ctrl+s`, `s`      | toggle_settings | Open the keybinding editor overlay              |
| `ctrl+o`, `o`      | toggle_sort     | Toggle domain-based sorting for search results  |
| `alt+1`            | tab_search      | Switch to the Search tab                        |
| `alt+2`            | tab_history     | Switch to the History tab                       |
| `alt+3`            | tab_rules       | Switch to the Rules tab                         |
| `alt+4`            | tab_add         | Switch to the Add tab                           |

### Mouse Controls

- **Left-click**: Select results or open tabs
- **Right-click**: Open context menu (open, delete, prioritize)
- **Scroll wheel**: Navigate through results
- **Scrollbar drag**: Quick scroll through long result lists

### Customizing TUI

TUI settings are stored in a separate `tui.yaml` file alongside your main config file. This file is automatically created with default values when you first run `hister search`.

**TUI config location**: `~/.config/hister/tui.yaml`

#### tui.yaml Structure

```yaml
# Theme settings
dark_theme: "dracula"
light_theme: "gruvbox"
color_scheme: "auto"
# themes_dir: "/path/to/custom/themes"  # optional

# TUI keybindings
hotkeys:
  ctrl+c: "quit"
  ctrl+t: "toggle_theme"
  ctrl+s: "toggle_settings"
  ctrl+o: "toggle_sort"
  alt+1: "tab_search"
  alt+2: "tab_history"
  alt+3: "tab_rules"
  alt+4: "tab_add"
  # ... and all other TUI keybindings
```

#### Available TUI Actions

- `quit` - Exit the TUI application
- `toggle_help` - Show/hide the help overlay
- `toggle_focus` - Switch between input and results views
- `scroll_up`/`scroll_down` - Move selection up/down
- `open_result` - Open selected URL in browser
- `delete_result` - Delete selected entry from index
- `toggle_theme` - Open theme picker
- `toggle_settings` - Open keybinding editor
- `toggle_sort` - Toggle sorting mode
- `tab_search`/`tab_history`/`tab_rules`/`tab_add` - Switch tabs

Note: After modifying `tui.yaml`, restart the `hister search` command to apply changes.

## Next Steps

- Explore the [advanced search syntax](https://blevesearch.com/docs/Query-String-Query/)
- Configure blacklist, hotkeys, sensitive data patterns and priority rules in your config file
- Set up keyword aliases for efficient searching
- Import your existing browser history

## Troubleshooting

### Server won't start

- Check if port 4433 is already in use
- Verify the configuration file syntax

### Extension not connecting

- Ensure the Hister server is running
- Verify the extension is configured with the correct server URL
- Check browser console for errors

### Import fails

- Ensure your server is running during import

## Nix

### Quick usage

Run directly from the repository:

```bash
nix run github:asciimoo/hister
```

Add to your current shell session:

```bash
nix shell github:asciimoo/hister
```

Install permanently to your user profile:

```bash
nix profile install github:asciimoo/hister
```

### Flake Setup

Add the input to your `flake.nix`:

```nix
{
  inputs.hister.url = "github:asciimoo/hister";

  outputs = { self, nixpkgs, hister, ... }: {
    # For NixOS:
    nixosConfigurations.yourHostname = nixpkgs.lib.nixosSystem {
      modules = [
        ./configuration.nix
        hister.nixosModules.default
      ];
    };

    # For Home-Manager:
    homeConfigurations."yourUsername" = home-manager.lib.homeManagerConfiguration {
      modules = [
        ./home.nix
        hister.homeModules.default
      ];
    };

    # For Darwin (macOS):
    darwinConfigurations."yourHostname" = darwin.lib.darwinSystem {
      modules = [
        ./configuration.nix
        hister.darwinModules.default
      ];
    };
  };
}
```

### Service Configuration

Enable and configure the service in your configuration file:

```nix
services.hister = {
  enable = true;

  # Optional: Set via Nix options (takes precedence over config file)
  # port = 4433;
  # dataDir = "/var/lib/hister";  # NixOS Recommend: "/var/lib/hister"
                                  # Home-Manager Recommend: "~/.local/share/hister"
                                  # Darwin Recommend: "~/Library/Application Support/hister"

  # Optional: Use existing YAML config file
  # configPath = /path/to/config.yml;

  # Optional: Inline configuration (converted to YAML)
  # Note: Only one of configPath or config can be used
  config = {
    app = {
      search_url = "https://google.com/search?q={query}";
      log_level = "info";
    };
    server = {
      address = "127.0.0.1:4433";
      database = "db.sqlite3";
    };
    hotkeys = {
      "/" = "focus_search_input";
      "enter" = "open_result";
      "alt+enter" = "open_result_in_new_tab";
      "alt+j" = "select_next_result";
      "alt+k" = "select_previous_result";
      "alt+o" = "open_query_in_search_engine";
    };
  };
};
```

**Notes:**

- The `port` and `dataDir` options override corresponding values in your config file
- To manage settings through the config file only, leave `port` and `dataDir` unset

### Add to Packages (Without Service)

If you don't want to use the module system, add the package directly:

**System packages (NixOS/Darwin):**

```nix
{ inputs, pkgs, ... }: {
  environment.systemPackages = [ inputs.hister.packages.${pkgs.stdenvNoCC.hostPlatform.system}.default ];
}
```

**User packages (Home-Manager):**

```nix
{ inputs, pkgs, ... }: {
  home.packages = [ inputs.hister.packages.${pkgs.stdenvNoCC.hostPlatform.system}.default ];
}
```
