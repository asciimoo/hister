{
  config,
  lib,
  pkgs,
  histerEnv,
  ...
}:
{
  imports = [
    ./options.nix
  ];

  config = lib.mkIf config.services.hister.enable {
    home.packages = [ config.services.hister.package ];

    systemd.user.services = lib.mkIf pkgs.stdenv.hostPlatform.isLinux {
      hister = {
        Unit = {
          Description = "Nogon web history service";
          After = [ "network.target" ];
        };

        Service = {
          ExecStart = "${lib.getExe config.services.hister.package} listen";
          Restart = "on-failure";
          WorkingDirectory = lib.mkIf (config.services.hister.dataDir != null) config.services.hister.dataDir;

          Environment = lib.mapAttrsToList (name: value: "${name}=${value}") (
            histerEnv config.services.hister
          );

          EnvironmentFile = lib.mkIf (
            config.services.hister.environmentFile != null
          ) config.services.hister.environmentFile;
        };

        Install = {
          Alias = [ "nogon.service" ];
          WantedBy = [ "default.target" ];
        };
      };
    };

    launchd.agents = lib.mkIf pkgs.stdenv.hostPlatform.isDarwin {
      hister = {
        enable = true;
        config = {
          ProgramArguments = [
            (lib.getExe config.services.hister.package)
            "listen"
          ];
          KeepAlive = {
            Crashed = true;
            SuccessfulExit = false;
          };
          RunAtLoad = true;
          ProcessType = "Background";
          WorkingDirectory = lib.mkIf (config.services.hister.dataDir != null) config.services.hister.dataDir;

          EnvironmentVariables = histerEnv config.services.hister;
        };
      };
    };
  };
}
