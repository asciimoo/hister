{
  lib,
  buildGoModule,
  sqlite,
  fetchNpmDeps,
  nodejs,
  histerRev ? "unknown",
}:
let
  packageJson = builtins.fromJSON (builtins.readFile ../ext/package.json);
  npmDeps = fetchNpmDeps {
    src = ../server/static/js;
    hash = "sha256-BupgGlAhzanFyjv43terHsUUjmAxFniwMSBLFi8shC0=";
  };
in
buildGoModule (finalAttrs: {
  pname = "hister";
  version = packageJson.version;

  src = ../.;

  vendorHash = "sha256-KEuZ+jKG3fMYymZr9fvwlTzLFVcYfUAofe8DOIqHUDY=";
  proxyVendor = true;

  nativeBuildInputs = [ nodejs ];

  buildInputs = [ sqlite ];

  postPatch = "";

  preBuild = ''
    # Build npm frontend
    # In goModules derivation, this runs but doesn't affect the build
    # In main derivation, this creates dist files before Go compilation
    cd server/static/js
    mkdir -p $TMPDIR/npm-cache
    cp -r ${npmDeps}/* $TMPDIR/npm-cache/
    export NPM_CONFIG_CACHE=$TMPDIR/npm-cache
    npm ci --offline
    npm run build
    cd ../../..

    export CGO_CFLAGS="-I${sqlite.dev}/include"
    export CGO_LDFLAGS="-L${sqlite.out}/lib -lsqlite3"
  '';

  ldflags = [
    "-s"
    "-w"
    "-X main.version=${finalAttrs.version}"
    "-X main.commit=${histerRev}"
  ];

  subPackages = [ "." ];

  doCheck = false;

  passthru = {
    inherit npmDeps;
  };

  meta = {
    description = "Web history on steroids - blazing fast, content-based search for visited websites";
    homepage = "https://github.com/asciimoo/hister";
    license = lib.licenses.agpl3Plus;
    maintainers = [ lib.maintainers.FlameFlag ];
    mainProgram = "hister";
    platforms = lib.platforms.unix;
  };
})
