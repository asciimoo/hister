---
date: '2026-07-14T00:00:00+02:00'
draft: false
title: 'Installing Nogon'
description: 'Install a release binary, build from source, or run Nogon with Docker, Nix, or Proxmox VE.'
---

The `nogon` program contains both the search server and the terminal client. For the fastest local setup, download a prebuilt binary and continue with the [quickstart guide](quickstart).

Downloads and package listings may still use the Hister name. If your installation provides `hister`, use that command in place of `nogon` in these guides.

If someone else already operates the Nogon server you use and you only search through the web interface, you do not need to install this program.

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

3. Rename the downloaded file to `nogon` or `nogon.exe`.

4. On Linux or macOS, open a terminal in the download directory and make the file executable:

   ```bash
   chmod +x nogon
   ```

5. Start the server:

   ```bash
   ./nogon listen
   ```

   On Windows, run `.\nogon.exe listen` instead.

6. Open <http://127.0.0.1:4433> in your browser, then continue with the [quickstart](quickstart) to install the browser extension and begin indexing.

Release pages also contain a checksums file that can be used to verify the download. Development snapshots are available from the [rolling release](https://github.com/asciimoo/hister/releases/tag/rolling), but stable releases are recommended for new users.

You may optionally move the binary to a directory on your `PATH`, such as `/usr/local/bin` or `~/.local/bin`.

## Building from source

Building Nogon requires Go 1.26, npm, and a C compiler for CGO dependencies.

```bash
git clone https://github.com/asciimoo/hister.git
cd hister
./manage.sh build
```

The build produces a `hister` binary in the repository root. Source is also mirrored on [Codeberg](https://codeberg.org/asciimoo/hister).

## Homebrew

If you use Homebrew, getting started with Nogon is quick and easy to do:

```bash
brew install hister
```

The formula currently installs the `hister` command. Start the server with `hister listen`.

## Docker

The official container is published at [GitHub Container Registry](https://github.com/asciimoo/hister/pkgs/container/hister). See the [Docker guide](docker) for a complete Compose setup, persistent storage, and reverse proxy examples.

## Systemd

For a manual Linux installation, install the Nogon executable at
`/usr/local/bin/nogon` and keep `hister` as an alias to that executable if you use
the old command. Copy `contrib/systemd/hister.service` from the repository to
`/etc/systemd/system/hister.service`.

The service uses the `hister` user and group, `/var/lib/hister` for data, and the
optional `/etc/hister/hister.env` environment file. On a fresh installation,
create that system user and group first. On an upgrade, keep the existing account,
paths, ownership, and environment file.

After updating an existing installation:

```bash
sudo systemctl daemon-reload
sudo systemctl reenable hister.service
sudo systemctl restart hister.service
```

For a fresh installation, use `sudo systemctl enable --now hister.service`.
Enabling the unit also creates the `nogon.service` alias. Commands such as
`systemctl status nogon.service` refer to the same running service. Keep the
installed filename `hister.service` to preserve existing service overrides.

## Nix

### Quick usage

Run the Nogon server directly from the repository:

```nix
nix run github:asciimoo/hister -- listen
```

Add Nogon to the current shell:

```nix
nix shell github:asciimoo/hister
```

Install it into your user profile:

```nix
nix profile install github:asciimoo/hister
```

The package provides both `nogon` and the compatible `hister` command. The
`default`, `nogon`, and `hister` package names select the same installation.
The repository address remains `github:asciimoo/hister`.

### Flake setup

Add the input to `flake.nix`:

```nix
{
  inputs.nogon.url = "github:asciimoo/hister";

  outputs = { self, nixpkgs, nogon, ... }: {
    nixosConfigurations.yourHostname = nixpkgs.lib.nixosSystem {
      modules = [
        ./configuration.nix
        nogon.nixosModules.default
      ];
    };

    homeConfigurations."yourUsername" = home-manager.lib.homeManagerConfiguration {
      modules = [
        ./home.nix
        nogon.homeModules.default
      ];
    };

    darwinConfigurations."yourHostname" = darwin.lib.darwinSystem {
      modules = [
        ./configuration.nix
        nogon.darwinModules.default
      ];
    };
  };
}
```

### Service configuration

Enable and configure the service in your configuration file:

```nix
services.nogon = {
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

Existing `services.hister` settings and `hister` module imports still work.
Both option names configure the same service; conflicting values produce a Nix
configuration error. Linux retains `hister.service`, with `nogon.service` as an
alias. Darwin retains the existing launchd agent name. Keep your current data
paths and environment files when upgrading.

To install only the package without enabling a service:

```nix
{ inputs, pkgs, ... }: {
  environment.systemPackages = [ inputs.nogon.packages.${pkgs.stdenvNoCC.hostPlatform.system}.default ];
}
```

For Home Manager:

```nix
{ inputs, pkgs, ... }: {
  home.packages = [ inputs.nogon.packages.${pkgs.stdenvNoCC.hostPlatform.system}.default ];
}
```

## Proxmox VE

Nogon is available through the [Proxmox VE Community Scripts](https://community-scripts.org/scripts/hister) project for LXC installations:

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/community-scripts/ProxmoxVED/main/ct/hister.sh)"
```

This installer is maintained by the community scripts project, not by Nogon. Review the script before running it on your Proxmox host.
