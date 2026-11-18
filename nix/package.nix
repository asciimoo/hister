{
  lib,
  buildGoModule,
  buildNpmPackage,
  importNpmLock,
  nix-update-script,
  sqlite,
  yt-dlp,
  makeBinaryWrapper,
  pkg-config,
  histerRev ? "unknown",
}:
let
  version = (builtins.fromJSON (builtins.readFile ../webui/app/package.json)).version;

  frontend = buildNpmPackage {
    pname = "nogon-frontend";
    inherit version;
    src = ../.;
    npmWorkspace = "webui/app";
    npmDeps = importNpmLock { npmRoot = ../.; };
    npmConfigHook = importNpmLock.npmConfigHook;
    dontNpmBuild = false;
    preBuild = ''
      patchShebangs webui
    '';
    installPhase = ''
      runHook preInstall
      mkdir -p "$out"
      cp -r webui/app/build/* "$out/"
      runHook postInstall
    '';
  };
in
buildGoModule (finalAttrs: {
  pname = "nogon";
  inherit version;

  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../nogon.go
      ../client
      ../server
      ../config
      ../files
      ../logging
      ../cmd
    ];
  };

  vendorHash = "sha256-BXs1VMZWxoLXvI2hDeiJoJIFwpz2xPCHg7NzegNrJ8o=";
  proxyVendor = true;

  nativeBuildInputs = [
    pkg-config
    makeBinaryWrapper
  ];
  buildInputs = [ sqlite ];

  tags = [ "libsqlite3" ];

  preBuild = ''
    mkdir -p server/static/app
    cp -r ${frontend}/* server/static/app/
  '';

  ldflags = [
    "-s"
    "-w"
    "-X main.version=${finalAttrs.version}"
    "-X main.commit=${histerRev}"
  ];

  subPackages = [ "." ];

  postInstall = ''
    wrapProgram "$out/bin/nogon" \
      --prefix PATH : ${lib.makeBinPath [ yt-dlp ]}
    ln -s nogon "$out/bin/hister"
  '';

  passthru = {
    inherit frontend;
    updateScript = nix-update-script {
      attrPath = "nogon";
      extraArgs = [
        "--flake"
        "--version=skip"
        "--no-src"
        "--build"
      ];
    };
  };

  meta = {
    description = "Web history on steroids - blazing fast, content-based search for visited websites";
    homepage = "https://github.com/asciimoo/nogon";
    license = lib.licenses.agpl3Plus;
    maintainers = [ lib.maintainers._4evy ];
    mainProgram = "nogon";
    platforms = lib.platforms.unix;
  };
})
