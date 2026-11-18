{
  description = "Nogon web history search";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    flake-parts.inputs.nixpkgs-lib.follows = "nixpkgs";
  };

  outputs =
    inputs:
    inputs.flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [
        "x86_64-linux"
        # "x86_64-darwin" # No longer supported in 26.11
        "aarch64-linux"
        "aarch64-darwin"
      ];

      perSystem =
        {
          config,
          self',
          inputs',
          pkgs,
          system,
          ...
        }:
        let
          nogonPackage = pkgs.callPackage ./nix/package.nix { nogonRev = inputs.self.rev or "unknown"; };
        in
        {
          packages.default = nogonPackage;
          packages.nogon = nogonPackage;
          packages.hister = nogonPackage;

          checks.compatibility = import ./nix/tests.nix {
            inherit pkgs;
            flake = inputs.self;
          };

          devShells.default = pkgs.mkShell {
            packages = builtins.attrValues { inherit (pkgs) go gopls gotools; };
          };
        };

      flake = {
        nixosModules.default = inputs.self.nixosModules.nogon;
        nixosModules.hister = inputs.self.nixosModules.nogon;
        nixosModules.nogon =
          {
            config,
            lib,
            pkgs,
            ...
          }:
          {
            key = "${inputs.self.outPath}/nixos-module";
            imports = [
              (lib.mkAliasOptionModule [ "services" "nogon" ] [ "services" "hister" ])
            ];
            services.hister.package = (
              lib.mkDefault inputs.self.packages.${pkgs.stdenvNoCC.hostPlatform.system}.default
            );
            systemd.services.hister = lib.mkIf config.services.hister.enable {
              aliases = [ "nogon.service" ];
            };
          };

        homeModules.default = inputs.self.homeModules.nogon;
        homeModules.hister = inputs.self.homeModules.nogon;
        homeModules.nogon =
          { lib, pkgs, ... }:
          {
            key = "${inputs.self.outPath}/home-module";
            imports = [ ./nix/home.nix ];
            services.hister.package = (
              lib.mkDefault inputs.self.packages.${pkgs.stdenvNoCC.hostPlatform.system}.default
            );
          };

        darwinModules.default = inputs.self.darwinModules.nogon;
        darwinModules.hister = inputs.self.darwinModules.nogon;
        darwinModules.nogon =
          { lib, pkgs, ... }:
          {
            key = "${inputs.self.outPath}/darwin-module";
            imports = [ ./nix/darwin.nix ];
            services.hister.package = (
              lib.mkDefault inputs.self.packages.${pkgs.stdenvNoCC.hostPlatform.system}.default
            );
          };
      };
    };
}
