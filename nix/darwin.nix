{
  config,
  lib,
  nogonEnv,
  ...
}:
{
  imports = [
    ./options.nix
  ];

  config = lib.mkIf config.services.hister.enable {
    environment.systemPackages = [ config.services.hister.package ];

    launchd.user.agents.hister = {
      serviceConfig = {
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
        EnvironmentVariables = nogonEnv config.services.hister;
      };
      managedBy = "services.hister.enable";
    };
  };
}
