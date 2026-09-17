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
    pname = "hister-frontend";
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
  pname = "hister";
  inherit version;

  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../hister.go
      ../client
      ../server
      ../config
      ../files
      ../cmd
    ];
  };

  vendorHash = "sha256-ZB3Dw/MCy9WNLWR9sBTkJShc7OjGStxqJYZPUp8Gs4U=";
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
    wrapProgram $out/bin/hister \
      --prefix PATH : ${lib.makeBinPath [ yt-dlp ]}
  '';

  passthru = {
    inherit frontend;
    updateScript = nix-update-script {
      attrPath = "hister";
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
    homepage = "https://github.com/asciimoo/hister";
    license = lib.licenses.agpl3Plus;
    maintainers = [ lib.maintainers._4evy ];
    mainProgram = "hister";
    platforms = lib.platforms.unix;
  };
})
