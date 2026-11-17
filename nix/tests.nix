{ pkgs, flake }:
let
  inherit (pkgs) lib;
  packages = flake.packages.${pkgs.stdenv.hostPlatform.system};
  package = packages.nogon or (throw "The Nogon package alias is missing");
  moduleFor = modules: name: modules.${name} or (throw "The ${name} module export is missing");
  nixos =
    names: services:
    (flake.inputs.nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = map (moduleFor flake.nixosModules) names ++ [ { inherit services; } ];
    }).config;
  checkNixos =
    name:
    let
      cfg = nixos [ name ] { ${if name == "default" then "nogon" else name}.enable = true; };
      service = cfg.systemd.services.hister;
    in
    lib.assertMsg (
      !(cfg.systemd.services ? nogon)
      && builtins.elem "nogon.service" service.aliases
      && service.serviceConfig.User == "hister"
      && service.serviceConfig.Group == "hister"
      && service.serviceConfig.StateDirectory == "hister"
      && service.environment.HISTER_DATA_DIR == "/var/lib/hister"
      && service.serviceConfig.NoNewPrivileges
      && service.serviceConfig.ProtectSystem == "strict"
      && lib.hasSuffix "/bin/nogon listen" service.serviceConfig.ExecStart
    ) "${name} must retain the single NixOS service and its storage identity";
  mixedNixos = nixos [ "hister" "nogon" ] {
    hister = {
      enable = true;
      port = 5544;
    };
    nogon = {
      dataDir = "/srv/existing-hister";
      environmentFile = "/run/secrets/hister.env";
      configPath = "/etc/hister/config.yml";
    };
  };
  mixedService = mixedNixos.systemd.services.hister;
  disabledNixos = nixos [ "nogon" ] { nogon.enable = false; };
  conflictingPorts =
    builtins.tryEval
      (nixos [ "nogon" ] {
        hister.port = 4433;
        nogon.port = 5544;
      }).services.hister.port;
  # Evaluate our Home Manager and Darwin modules at their declared output boundary.
  # Native activation is covered by the platform's service manager, not this check.
  userConfig =
    kind: system: names: services:
    (lib.evalModules {
      specialArgs.pkgs = flake.inputs.nixpkgs.legacyPackages.${system};
      modules = map (moduleFor flake.${kind}) names ++ [
        {
          options = {
            assertions = lib.mkOption {
              type = lib.types.listOf lib.types.anything;
              default = [ ];
            };
            warnings = lib.mkOption {
              type = lib.types.listOf lib.types.str;
              default = [ ];
            };
            home.packages = lib.mkOption {
              type = lib.types.listOf lib.types.package;
              default = [ ];
            };
            environment.systemPackages = lib.mkOption {
              type = lib.types.listOf lib.types.package;
              default = [ ];
            };
            systemd.user.services = lib.mkOption {
              type = lib.types.attrsOf lib.types.anything;
              default = { };
            };
            launchd.agents = lib.mkOption {
              type = lib.types.attrsOf lib.types.anything;
              default = { };
            };
            launchd.user.agents = lib.mkOption {
              type = lib.types.attrsOf lib.types.anything;
              default = { };
            };
          };
          config = { inherit services; };
        }
      ];
    }).config;
  userSettings = {
    enable = true;
    dataDir = "/srv/existing-hister";
    port = 5544;
    configPath = "/etc/hister/config.yml";
    environmentFile = "/run/secrets/hister.env";
  };
  checkUserModule =
    kind: system: names: services:
    let
      cfg = userConfig kind system names services;
      linux = system == "x86_64-linux";
      agents = if kind == "homeModules" then cfg.launchd.agents else cfg.launchd.user.agents;
      service =
        if linux then
          cfg.systemd.user.services.hister.Service
        else if kind == "homeModules" then
          agents.hister.config
        else
          agents.hister.serviceConfig;
      env =
        if linux then
          service.Environment
        else
          lib.mapAttrsToList (n: v: "${n}=${v}") service.EnvironmentVariables;
    in
    lib.assertMsg (
      (
        if linux then
          builtins.attrNames cfg.systemd.user.services == [ "hister" ]
          && builtins.elem "nogon.service" cfg.systemd.user.services.hister.Install.Alias
          && service.EnvironmentFile == "/run/secrets/hister.env"
          && lib.hasSuffix "/bin/nogon listen" service.ExecStart
        else
          builtins.attrNames agents == [ "hister" ]
          && lib.hasSuffix "/bin/nogon" (builtins.head service.ProgramArguments)
          && builtins.elem "listen" service.ProgramArguments
      )
      && service.WorkingDirectory == "/srv/existing-hister"
      && builtins.all (value: builtins.elem value env) [
        "HISTER_DATA_DIR=/srv/existing-hister"
        "HISTER_PORT=5544"
        "HISTER_CONFIG=/etc/hister/config.yml"
      ]
      && builtins.all (a: a.assertion) cfg.assertions
    ) "${kind} on ${system} must retain one service and the existing paths";
  checkUserNames =
    kind: system:
    builtins.all
      (
        name:
        checkUserModule kind system [ name ] {
          ${if name == "default" then "nogon" else name} = userSettings;
        }
      )
      [
        "hister"
        "nogon"
        "default"
      ]
    && checkUserModule kind system [ "hister" "nogon" ] {
      hister = userSettings;
      nogon.enable = true;
    };
in
assert lib.assertMsg (
  packages.default.outPath == package.outPath
) "Nogon must be the default package";
assert lib.assertMsg (
  packages.hister.outPath == package.outPath
) "Hister must remain the same package";
assert lib.assertMsg (package.meta.mainProgram == "nogon") "nix run must select the Nogon command";
assert builtins.all checkNixos [
  "hister"
  "nogon"
  "default"
];
assert lib.assertMsg (
  !(disabledNixos.systemd.services ? hister) && !(disabledNixos.systemd.services ? nogon)
) "Disabled modules must not create a service";
assert lib.assertMsg (!conflictingPorts.success) "Conflicting old and new options must be rejected";
assert lib.assertMsg (
  !(mixedNixos.systemd.services ? nogon)
  && mixedService.environment.HISTER_DATA_DIR == "/srv/existing-hister"
  && mixedService.environment.HISTER_PORT == "5544"
  && mixedService.environment.HISTER_CONFIG == "/run/hister/config.yml"
  && mixedService.serviceConfig.EnvironmentFile == "/run/secrets/hister.env"
  && mixedService.serviceConfig.ReadWritePaths == [ "/srv/existing-hister" ]
  && builtins.elem "/etc/hister/config.yml:/run/hister/config.yml" mixedService.serviceConfig.BindReadOnlyPaths
) "Mixed NixOS option and module names must configure the same service";
assert checkUserNames "homeModules" "x86_64-linux";
assert checkUserNames "homeModules" "aarch64-darwin";
assert checkUserNames "darwinModules" "aarch64-darwin";
pkgs.runCommand "nogon-package-compatibility"
  {
    nativeBuildInputs = [ pkgs.makeBinaryWrapper ];
  }
  ''
    mkdir -p "$out/bin"
    # Exercise the actual install hook with a small executable fixture.
    cat > "$out/bin/hister" <<'EOF'
    #!${pkgs.runtimeShell}
    command -v yt-dlp
    printf '%s\n' "$@"
    EOF
    chmod +x "$out/bin/hister"
    ${package.postInstall}

    test -x "$out/bin/nogon"
    test -L "$out/bin/hister"
    test "$out/bin/hister" -ef "$out/bin/nogon"
    expected=$(printf '%s\n' '${lib.getExe pkgs.yt-dlp}' 'argument with spaces')
    test "$("$out/bin/nogon" 'argument with spaces')" = "$expected"
    test "$("$out/bin/hister" 'argument with spaces')" = "$expected"
  ''
