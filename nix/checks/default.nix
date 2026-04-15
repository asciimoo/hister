{
  pkgs,
  system,
  inputs,
}:
let
  lib = pkgs.lib;
  isLinux = pkgs.stdenv.hostPlatform.isLinux;
  isDarwin = pkgs.stdenv.hostPlatform.isDarwin;
  args = {
    inherit
      pkgs
      inputs
      lib
      ;
  };
in
lib.optionalAttrs isLinux (import ./nixos.nix args)
// lib.optionalAttrs isDarwin (import ./darwin.nix args)
// import ./home.nix args
