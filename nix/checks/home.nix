{
  pkgs,
  inputs,
  lib,
}:
# home-manager checks. We build the real activationPackage and grep the
# generated unit/plist files inside it, following the same pattern as
# home-manager/tests/modules/services/* (see syncthing/extra-options.nix)
let
  isLinux = pkgs.stdenv.hostPlatform.isLinux;
  isDarwin = pkgs.stdenv.hostPlatform.isDarwin;

  evalHister =
    module:
    inputs.home-manager.lib.homeManagerConfiguration {
      inherit pkgs;
      modules = [
        inputs.self.homeModules.hister
        module
        (
          { lib, ... }:
          {
            services.hister.package = lib.mkForce pkgs.hello;
            home.username = "tester";
            home.homeDirectory = if isDarwin then "/Users/tester" else "/home/tester";
            home.stateVersion = "24.11";
          }
        )
      ];
    };

  activationOf = eval: eval.activationPackage;

  # Path to the generated service artefact inside the activationPackage
  unitPath =
    if isLinux then
      "home-files/.config/systemd/user/hister.service"
    else
      "LaunchAgents/org.nix-community.home.hister.plist";
in
{
  # Build the activation for a realistic configuration to catch
  # cross-cutting wiring issues
  home-activation =
    (evalHister {
      services.hister.enable = true;
      services.hister.settings.app.log_level = "debug";
    }).activationPackage;

  # Default service/plist is rendered and runs `hister listen`
  home-defaults =
    let
      eval = evalHister { services.hister.enable = true; };
    in
    pkgs.runCommand "hister-home-defaults"
      {
        activation = activationOf eval;
      }
      ''
        unit=$activation/${unitPath}
        test -e "$unit" || { echo "missing $unit" >&2; exit 1; }
        grep -qF 'listen' "$unit"
        touch $out
      '';

  # settings get rendered to YAML and pointed at via HISTER_CONFIG
  home-settings =
    let
      eval = evalHister {
        services.hister.enable = true;
        services.hister.settings.app.log_level = "debug";
      };
      cfg = eval.config;
      yamlPath =
        if isLinux then
          # Environment is a list of KEY=VALUE strings; pick out HISTER_CONFIG.
          let
            entries = cfg.systemd.user.services.hister.Service.Environment or [ ];
            match = lib.findFirst (lib.hasPrefix "HISTER_CONFIG=") null entries;
          in
          if match == null then null else lib.removePrefix "HISTER_CONFIG=" match
        else
          cfg.launchd.agents.hister.config.EnvironmentVariables.HISTER_CONFIG or null;
    in
    assert yamlPath != null;
    pkgs.runCommand "hister-home-settings"
      {
        activation = activationOf eval;
        yaml = yamlPath;
      }
      ''
        unit=$activation/${unitPath}
        grep -qF 'HISTER_CONFIG'    "$unit"
        grep -qF 'log_level: debug' "$yaml"
        touch $out
      '';
}
// lib.optionalAttrs isLinux {
  # systemd-only feature, skipped on darwin home-manager
  home-env-file =
    let
      eval = evalHister {
        services.hister.enable = true;
        services.hister.environmentFile = "/run/secrets/hister.env";
      };
    in
    pkgs.runCommand "hister-home-env-file"
      {
        activation = activationOf eval;
      }
      ''
        unit=$activation/${unitPath}
        grep -qF '/run/secrets/hister.env' "$unit"
        touch $out
      '';
}
