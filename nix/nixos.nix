{
  config,
  lib,
  histerEnv,
  ...
}:
{
  imports = [
    ./options.nix
  ];

  options.services.hister = {
    user = lib.mkOption {
      type = lib.types.str;
      default = "hister";
      description = "User account under which Hister runs.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "hister";
      description = "Group under which Hister runs.";
    };

    openFirewall = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = ''
        Whether to open `services.hister.port` in the firewall. Has no
        effect if `port` is null.
      '';
    };
  };

  config = lib.mkIf config.services.hister.enable {
    environment.systemPackages = [ config.services.hister.package ];

    users.users = lib.mkIf (config.services.hister.user == "hister") {
      hister = {
        description = "Hister web history service";
        group = config.services.hister.group;
        isSystemUser = true;
      };
    };

    users.groups = lib.mkIf (config.services.hister.group == "hister") {
      hister = { };
    };

    systemd.services.hister = {
      description = "Hister web history service";
      after = [ "network.target" ];
      wantedBy = [ "multi-user.target" ];

      environment =
        histerEnv config.services.hister
        // lib.optionalAttrs (config.services.hister.dataDir == null) {
          HISTER_DATA_DIR = "/var/lib/hister";
        };

      serviceConfig = {
        ExecStart = "${lib.getExe config.services.hister.package} listen";
        Restart = "on-failure";
        User = config.services.hister.user;
        Group = config.services.hister.group;
        StateDirectory = lib.mkIf (config.services.hister.dataDir == null) "hister";
        ReadWritePaths = lib.mkIf (config.services.hister.dataDir != null) [
          config.services.hister.dataDir
        ];
        EnvironmentFile = lib.mkIf (
          config.services.hister.environmentFile != null
        ) config.services.hister.environmentFile;

        AmbientCapabilities = lib.mkIf (
          config.services.hister.port != null && config.services.hister.port < 1024
        ) [ "CAP_NET_BIND_SERVICE" ];
        CapabilityBoundingSet =
          if (config.services.hister.port != null && config.services.hister.port < 1024) then
            [ "CAP_NET_BIND_SERVICE" ]
          else
            [ "" ];

        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectKernelLogs = true;
        ProtectControlGroups = true;
        ProtectClock = true;
        ProtectHostname = true;
        ProtectProc = "invisible";
        ProcSubset = "pid";
        LockPersonality = true;
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        RestrictAddressFamilies = [
          "AF_INET"
          "AF_INET6"
          "AF_UNIX"
        ];
        SystemCallArchitectures = "native";
        SystemCallFilter = [
          "@system-service"
          "~@privileged"
        ];
        MemoryDenyWriteExecute = true;
        UMask = "0077";
      };
    };

    networking.firewall.allowedTCPPorts = lib.mkIf (
      config.services.hister.openFirewall && config.services.hister.port != null
    ) [ config.services.hister.port ];
  };
}
