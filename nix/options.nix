{
  config,
  lib,
  pkgs,
  ...
}:
let
  yamlFormat = pkgs.formats.yaml { };
  cfg = config.services.hister;
  mkNogonEnv =
    cfg:
    lib.optionalAttrs (cfg.dataDir != null) {
      HISTER_DATA_DIR = cfg.dataDir;
    }
    // lib.optionalAttrs (cfg.port != null) {
      HISTER_PORT = builtins.toString cfg.port;
    }
    // lib.optionalAttrs (cfg.configPath != null) {
      HISTER_CONFIG = builtins.toString cfg.configPath;
    }
    // lib.optionalAttrs (cfg.settings != { }) {
      HISTER_CONFIG = "${yamlFormat.generate "hister-config.yml" cfg.settings}";
    };
in
{
  imports = [
    (lib.mkAliasOptionModule [ "services" "nogon" ] [ "services" "hister" ])
    (lib.mkRenamedOptionModule [ "services" "hister" "config" ] [ "services" "hister" "settings" ])
  ];

  options.services.hister = {
    enable = lib.mkEnableOption "Nogon web history service";

    package = lib.mkOption {
      type = lib.types.package;
      description = "The Nogon package to use.";
    };

    dataDir = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      example = "/var/lib/hister";
      description = ''
        Directory where Nogon stores its data.
        If set, this will override the `app.directory` setting in the configuration file.
      '';
    };

    port = lib.mkOption {
      type = lib.types.nullOr lib.types.port;
      default = null;
      example = 4433;
      description = ''
        Port on which Nogon listens.
        If set, this will override the `server.address` port in the configuration file.
      '';
    };

    configPath = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      example = "/etc/hister/config.yml";
      description = "Path to an existing configuration file.";
    };

    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      example = "/run/secrets/hister.env";
      description = ''
        Path to an environment file (read at service start) used to inject
        secrets such as `HISTER__APP__ACCESS_TOKEN` without placing them in
        the world-readable Nix store. Only honored by the systemd-based
        services (NixOS and Linux home-manager); ignored on launchd.
      '';
    };

    settings = lib.mkOption {
      type = yamlFormat.type;
      default = { };
      description = ''
        Nogon configuration rendered to YAML and passed via HISTER_CONFIG.
        Accepts any structure the server accepts. See the `app`, `server`,
        `indexer`, `crawler`, `hotkeys`, `extractors`, and
        `sensitive_content_patterns` blocks documented upstream.
      '';
      example = lib.literalExpression ''
        {
          app = {
            search_url = "https://google.com/search?q={query}";
            log_level = "info";
          };
          server = {
            address = "127.0.0.1:4433";
            database = "db.sqlite3";
          };
          hotkeys.web = {
            "/" = "focus_search_input";
            "enter" = "open_result";
          };
        }
      '';
    };
  };

  config = {
    assertions = [
      {
        assertion = !(cfg.configPath != null && cfg.settings != { });
        message = "Only one of services.hister.configPath and services.hister.settings can be set";
      }
    ];
    _module.args.nogonEnv = mkNogonEnv;
  };
}
