{
  pkgs,
  inputs,
  lib,
}:
# NixOS-side checks. Each check builds a real NixOS configuration from
# inputs.self.nixosModules.hister and inspects the actual rendered unit file or
# asserts against config values directly, same pattern as the tests in
# nixpkgs/nixos/tests
let
  evalHister =
    module:
    inputs.nixpkgs.lib.nixosSystem {
      inherit (pkgs.stdenv.hostPlatform) system;
      modules = [
        inputs.self.nixosModules.hister
        module
        (
          { lib, ... }:
          {
            # Avoid needing fileSystems/bootloader wiring for eval-only checks
            boot.isContainer = true;
            system.stateVersion = "24.11";
            services.hister.package = lib.mkForce pkgs.hello;
          }
        )
      ];
    };

  unitText = eval: eval.config.systemd.units."hister.service".text;

  failingAssertions =
    eval:
    lib.concatMapStringsSep "\n" (a: a.message) (lib.filter (a: !a.assertion) eval.config.assertions);
in
{
  # End-to-end VM boot
  nixos-vm = pkgs.testers.runNixOSTest {
    name = "hister";
    nodes.machine =
      { ... }:
      {
        imports = [ inputs.self.nixosModules.hister ];
        services.hister = {
          enable = true;
          port = 4433;
          settings.app.log_level = "debug";
        };
        system.stateVersion = "24.11";
      };
    testScript = ''
      machine.wait_for_unit("hister.service")
      machine.wait_for_open_port(4433)
      machine.succeed("curl -fsS http://localhost:4433/ >/dev/null")
    '';
  };

  # Default unit carries the auto-created user/group and the hardening flags
  nixos-defaults =
    let
      eval = evalHister { services.hister.enable = true; };
    in
    pkgs.runCommand "hister-nixos-defaults"
      {
        unit = pkgs.writeText "hister.service" (unitText eval);
      }
      ''
        grep -qF 'User=hister'                 "$unit"
        grep -qF 'Group=hister'                "$unit"
        grep -qF 'NoNewPrivileges=true'        "$unit"
        grep -qF 'ProtectSystem=strict'        "$unit"
        grep -qF 'MemoryDenyWriteExecute=true' "$unit"
        grep -qF 'StateDirectory=hister'       "$unit"
        grep -qF 'HISTER_DATA_DIR=/var/lib/hister' "$unit"
        touch $out
      '';

  # Privileged port grants CAP_NET_BIND_SERVICE and opens the firewall
  nixos-privileged-port =
    let
      eval = evalHister {
        services.hister.enable = true;
        services.hister.port = 443;
        services.hister.openFirewall = true;
      };
    in
    pkgs.runCommand "hister-nixos-privileged-port"
      {
        unit = pkgs.writeText "hister.service" (unitText eval);
        ports = lib.concatStringsSep " " (map toString eval.config.networking.firewall.allowedTCPPorts);
      }
      ''
        grep -qF 'HISTER_PORT=443'      "$unit"
        grep -qF 'CAP_NET_BIND_SERVICE' "$unit"
        [[ " $ports " == *" 443 "* ]]
        touch $out
      '';

  # High port leaves the capability off
  nixos-high-port =
    let
      eval = evalHister {
        services.hister.enable = true;
        services.hister.port = 8080;
      };
    in
    pkgs.runCommand "hister-nixos-high-port"
      {
        unit = pkgs.writeText "hister.service" (unitText eval);
      }
      ''
        grep -qF 'HISTER_PORT=8080' "$unit"
        if grep -qF 'CAP_NET_BIND_SERVICE' "$unit"; then
          echo "CAP_NET_BIND_SERVICE should not be set for high ports" >&2
          exit 1
        fi
        touch $out
      '';

  # Custom dataDir swaps StateDirectory for ReadWritePaths
  nixos-data-dir =
    let
      eval = evalHister {
        services.hister.enable = true;
        services.hister.dataDir = "/var/lib/hister-data";
      };
    in
    pkgs.runCommand "hister-nixos-data-dir"
      {
        unit = pkgs.writeText "hister.service" (unitText eval);
      }
      ''
        grep -qF 'HISTER_DATA_DIR=/var/lib/hister-data' "$unit"
        grep -qF 'ReadWritePaths=/var/lib/hister-data'  "$unit"
        if grep -qF 'StateDirectory=' "$unit"; then
          echo "StateDirectory should be absent when dataDir is set" >&2
          exit 1
        fi
        touch $out
      '';

  # environmentFile wires into EnvironmentFile=
  nixos-env-file =
    let
      eval = evalHister {
        services.hister.enable = true;
        services.hister.environmentFile = "/run/secrets/hister.env";
      };
    in
    pkgs.runCommand "hister-nixos-env-file"
      {
        unit = pkgs.writeText "hister.service" (unitText eval);
      }
      ''
        grep -qF 'EnvironmentFile=/run/secrets/hister.env' "$unit"
        touch $out
      '';

  # External configPath is passed through as HISTER_CONFIG
  nixos-config-path =
    let
      eval = evalHister {
        services.hister.enable = true;
        services.hister.configPath = "/etc/hister/config.yml";
      };
    in
    pkgs.runCommand "hister-nixos-config-path"
      {
        unit = pkgs.writeText "hister.service" (unitText eval);
      }
      ''
        grep -qF 'HISTER_CONFIG=/etc/hister/config.yml' "$unit"
        touch $out
      '';

  # settings get rendered to YAML in the store and pointed at via HISTER_CONFIG
  nixos-settings =
    let
      eval = evalHister {
        services.hister.enable = true;
        services.hister.settings = {
          app.log_level = "debug";
          extractors = [
            "title"
            "description"
          ];
        };
      };
    in
    pkgs.runCommand "hister-nixos-settings"
      {
        unit = pkgs.writeText "hister.service" (unitText eval);
        yaml = eval.config.systemd.services.hister.environment.HISTER_CONFIG;
      }
      ''
        grep -qF 'HISTER_CONFIG=' "$unit"
        grep -qF 'log_level: debug' "$yaml"
        grep -qFe '- title'       "$yaml"
        grep -qFe '- description' "$yaml"
        touch $out
      '';

  # Mutually-exclusive options fire an assertion
  nixos-config-conflict =
    let
      eval = evalHister {
        services.hister.enable = true;
        services.hister.configPath = "/etc/hister/config.yml";
        services.hister.settings.app.log_level = "info";
      };
    in
    pkgs.runCommand "hister-nixos-config-conflict"
      {
        messages = pkgs.writeText "assertions" (failingAssertions eval);
      }
      ''
        grep -qF 'Only one of services.hister.configPath and services.hister.settings' "$messages"
        touch $out
      '';
}
