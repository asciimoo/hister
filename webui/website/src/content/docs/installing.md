---
date: '2026-07-14T00:00:00+02:00'
draft: false
title: 'Installing Hister'
description: 'Install a release binary, build from source, or run Hister with Docker, Nix, or Proxmox VE.'
---

The `hister` program contains both the search server and the terminal client. For the fastest local setup, download a prebuilt binary and continue with the [quickstart guide](quickstart).

If someone else already operates the Hister server you use and you only search through the web interface, you do not need to install this program.

## Prebuilt binary

1. Open the [latest stable release](https://github.com/asciimoo/hister/releases/latest).

2. Download the file that matches your system:

   | System  | Processor           | Filename ending     |
   | ------- | ------------------- | ------------------- |
   | Linux   | Intel or AMD 64 bit | `linux_amd64`       |
   | Linux   | ARM 64 bit          | `linux_arm64`       |
   | macOS   | Apple silicon       | `darwin_arm64`      |
   | macOS   | Intel               | `darwin_amd64`      |
   | Windows | Intel or AMD 64 bit | `windows_amd64.exe` |

3. Rename the downloaded file to `hister` or `hister.exe`.

4. On Linux or macOS, open a terminal in the download directory and make the file executable:

   ```bash
   chmod +x hister
   ```

5. Start the server:

   ```bash
   ./hister listen
   ```

   On Windows, run `hister.exe listen` instead.

6. Open <http://127.0.0.1:4433> in your browser, then continue with the [quickstart](quickstart) to install the browser extension and begin indexing.

Release pages also contain a checksums file that can be used to verify the download. Development snapshots are available from the [rolling release](https://github.com/asciimoo/hister/releases/tag/rolling), but stable releases are recommended for new users.

You may optionally move the binary to a directory on your `PATH`, such as `/usr/local/bin` or `~/.local/bin`.

### Start automatically on macOS

On macOS, Hister can run as a per-user `launchd` agent. It starts when you log
in and is restarted if it exits unexpectedly.

From a source checkout, build Hister first:

```bash
./manage.sh build
```

Install the agent with the helper in the repository:

```bash
contrib/launchd/hister-launchd.sh install
```

The helper uses the `hister` binary in the repository root. To use a binary in
another location or a specific configuration file:

```bash
HISTER_BIN="$HOME/bin/hister" \
HISTER_CONFIG="$HOME/Library/Preferences/hister/config.yml" \
contrib/launchd/hister-launchd.sh install
```

Hister otherwise uses its normal configuration search paths. To override the
data directory for this service, set `HISTER_DATA_DIR`. Relative paths are
resolved when the agent is installed.

The agent is installed at:

```text
~/Library/LaunchAgents/org.hister.server.plist
```

Use the helper to manage the agent:

```bash
contrib/launchd/hister-launchd.sh status
contrib/launchd/hister-launchd.sh restart
contrib/launchd/hister-launchd.sh stop
contrib/launchd/hister-launchd.sh start
contrib/launchd/hister-launchd.sh logs
```

To remove only the background service while preserving the indexed data:

```bash
contrib/launchd/hister-launchd.sh uninstall
```

The Nix Darwin and Home Manager modules provide their own `launchd`
configuration for Nix installations.

## Building from source

Building Hister requires Go 1.26, npm, and a C compiler for CGO dependencies.

```bash
git clone https://github.com/asciimoo/hister.git
cd hister
./manage.sh build
```

The build produces a `hister` binary in the repository root. Source is also mirrored on [Codeberg](https://codeberg.org/asciimoo/hister).

## Docker

The official container is published at [GitHub Container Registry](https://github.com/asciimoo/hister/pkgs/container/hister). See the [Docker guide](docker) for a complete Compose setup, persistent storage, and reverse proxy examples.

## Nix

### Quick usage

Run the Hister server directly from the repository:

```nix
nix run github:asciimoo/hister -- listen
```

Add Hister to the current shell:

```nix
nix shell github:asciimoo/hister
```

Install it into your user profile:

```nix
nix profile install github:asciimoo/hister
```

### Flake setup

Add the input to `flake.nix`:

```nix
{
  inputs.hister.url = "github:asciimoo/hister";

  outputs = { self, nixpkgs, hister, ... }: {
    nixosConfigurations.yourHostname = nixpkgs.lib.nixosSystem {
      modules = [
        ./configuration.nix
        hister.nixosModules.default
      ];
    };

    homeConfigurations."yourUsername" = home-manager.lib.homeManagerConfiguration {
      modules = [
        ./home.nix
        hister.homeModules.default
      ];
    };

    darwinConfigurations."yourHostname" = darwin.lib.darwinSystem {
      modules = [
        ./configuration.nix
        hister.darwinModules.default
      ];
    };
  };
}
```

### Service configuration

Enable and configure the service in your configuration file:

```nix
services.hister = {
  enable = true;

  # Optional: Set via Nix options. These take precedence over the config file.
  # port = 4433;
  # dataDir = "/var/lib/hister";
  # openFirewall = true; # NixOS only
  # configPath = /path/to/config.yml;
  # environmentFile = "/run/secrets/hister.env";

  settings = {
    app = {
      search_url = "https://google.com/search?q={query}";
      log_level = "info";
    };
    server = {
      address = "127.0.0.1:4433";
      database = "db.sqlite3";
    };
  };
};
```

The NixOS module uses a hardened systemd service. The Linux Home Manager module uses a systemd user service, while the Darwin modules use a launchd agent. Use `environmentFile` for secrets on supported Linux services instead of placing them in the world readable Nix store.

To install only the package without enabling a service:

```nix
{ inputs, pkgs, ... }: {
  environment.systemPackages = [ inputs.hister.packages.${pkgs.stdenvNoCC.hostPlatform.system}.default ];
}
```

For Home Manager:

```nix
{ inputs, pkgs, ... }: {
  home.packages = [ inputs.hister.packages.${pkgs.stdenvNoCC.hostPlatform.system}.default ];
}
```

## Proxmox VE

Hister is available through the [Proxmox VE Community Scripts](https://community-scripts.org/scripts/hister) project for LXC installations:

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/community-scripts/ProxmoxVED/main/ct/hister.sh)"
```

This installer is maintained by the community scripts project, not by Hister. Review the script before running it on your Proxmox host.
