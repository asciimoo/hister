{
  pkgs,
  inputs,
  lib,
}:
# nix-darwin checks. We build the real darwin system toplevel and grep
# the generated LaunchAgent plist, mirroring the pattern in
# nix-darwin/tests (see tests/launchd-daemons.nix)
let
  evalHister =
    module:
    inputs.nix-darwin.lib.darwinSystem {
      system = pkgs.stdenv.hostPlatform.system;
      modules = [
        inputs.self.darwinModules.hister
        module
        (
          { config, lib, ... }:
          {
            services.hister.package = lib.mkForce pkgs.hello;
            system.stateVersion = lib.mkDefault config.system.maxStateVersion;
            system.primaryUser = "tester";
          }
        )
      ];
    };

  plistOf = eval: eval.config.environment.userLaunchAgents."org.nixos.hister.plist".source;
in
{
  # Build the full darwin toplevel for a realistic configuration, to catch
  # any wiring problem that doesn't show up at the unit level alone.
  darwin-system =
    (evalHister {
      services.hister.enable = true;
      services.hister.settings.app.log_level = "debug";
    }).system;

  # Default agent plist contains the expected ProgramArguments
  darwin-defaults =
    let
      eval = evalHister { services.hister.enable = true; };
    in
    pkgs.runCommand "hister-darwin-defaults"
      {
        plist = plistOf eval;
      }
      ''
        grep -qF 'ProgramArguments' "$plist"
        grep -qF 'listen'           "$plist"
        grep -qF 'RunAtLoad'        "$plist"
        touch $out
      '';

  # Custom dataDir wires into WorkingDirectory and HISTER_DATA_DIR
  darwin-data-dir =
    let
      eval = evalHister {
        services.hister.enable = true;
        services.hister.dataDir = "/Users/tester/Library/Hister";
      };
    in
    pkgs.runCommand "hister-darwin-data-dir"
      {
        plist = plistOf eval;
      }
      ''
        grep -qF 'WorkingDirectory'                        "$plist"
        grep -qF '/Users/tester/Library/Hister'            "$plist"
        grep -qF 'HISTER_DATA_DIR'                         "$plist"
        touch $out
      '';

  # settings rendered into a YAML file referenced by HISTER_CONFIG
  darwin-settings =
    let
      eval = evalHister {
        services.hister.enable = true;
        services.hister.settings.app.log_level = "debug";
      };
      envVars = eval.config.launchd.user.agents.hister.serviceConfig.EnvironmentVariables or { };
    in
    pkgs.runCommand "hister-darwin-settings"
      {
        plist = plistOf eval;
        yaml = envVars.HISTER_CONFIG;
      }
      ''
        grep -qF 'HISTER_CONFIG'    "$plist"
        grep -qF 'log_level: debug' "$yaml"
        touch $out
      '';
}
