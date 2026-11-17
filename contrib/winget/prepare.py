#!/usr/bin/env python3
"""Prepare local WinGet manifests from a stable release ZIP. Never publish them."""

import argparse
import hashlib
import re
import zipfile
from pathlib import Path


def digest(stream):
    checksum = hashlib.sha256()
    for block in iter(lambda: stream.read(1024 * 1024), b""):
        checksum.update(block)
    return checksum.hexdigest().upper()


def prepare(archive, output):
    match = re.fullmatch(r"nogon_([0-9]+\.[0-9]+\.[0-9]+)_windows_amd64\.zip", archive.name)
    if not match:
        raise ValueError("expected a stable Windows amd64 release archive: nogon_VERSION_windows_amd64.zip")
    version = match[1]
    with archive.open("rb") as source:
        checksum = digest(source)
        source.seek(0)
        with zipfile.ZipFile(source) as package:
            if sorted(package.namelist()) != ["hister.exe", "nogon.exe"]:
                raise ValueError("archive must contain only nogon.exe and hister.exe at its root")
            with package.open("nogon.exe") as nogon, package.open("hister.exe") as hister:
                if digest(nogon) != digest(hister):
                    raise ValueError("nogon.exe and hister.exe must be identical")

    identifier = "asciimoo.hister"
    repository = "https://github.com/asciimoo/hister"
    manifests = {
        "version": ("", "DefaultLocale: en-US\n"),
        "installer": (".installer", f"""InstallerType: zip
NestedInstallerType: portable
NestedInstallerFiles:
  - RelativeFilePath: nogon.exe
    PortableCommandAlias: nogon
  - RelativeFilePath: hister.exe
    PortableCommandAlias: hister
Commands:
  - nogon
  - hister
Installers:
  - Architecture: x64
    InstallerUrl: {repository}/releases/download/v{version}/{archive.name}
    InstallerSha256: {checksum}
"""),
        "defaultLocale": (".locale.en-US", f"""PackageLocale: en-US
Publisher: asciimoo
PublisherUrl: https://github.com/asciimoo
PublisherSupportUrl: {repository}/issues
Author: Adam Tauber
PackageName: Nogon
PackageUrl: {repository}
License: AGPL-3.0
LicenseUrl: {repository}/blob/HEAD/LICENSE
ShortDescription: Your own search engine for files and browsing history.
Description: Nogon, formerly Hister, indexes your files and visited web pages for private full text search.
Tags:
  - browser-history
  - go
  - golang
  - history
  - index
  - mcp
  - mcp-server
  - personal-search
  - personal-search-engine
  - privacy
  - search
  - search-engine
  - semantic-search
  - web
ReleaseNotesUrl: {repository}/releases/tag/v{version}
Documentations:
  - DocumentLabel: Documentation
    DocumentUrl: https://nogon.eu/docs
"""),
    }
    # Refuse to overwrite a maintainer's reviewed or edited manifests.
    if output.exists():
        raise ValueError(f"output directory already exists: {output}")
    output.mkdir(parents=True)
    for kind, (suffix, fields) in manifests.items():
        text = (
            f"# yaml-language-server: $schema=https://aka.ms/winget-manifest.{kind}.1.12.0.schema.json\n\n"
            f"PackageIdentifier: {identifier}\nPackageVersion: {version}\n"
            f"{fields}ManifestType: {kind}\nManifestVersion: 1.12.0\n"
        )
        (output / f"{identifier}{suffix}.yaml").write_text(text, encoding="utf-8")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", type=Path, help="GoReleaser's stable Windows amd64 ZIP")
    parser.add_argument("output", type=Path, help="new directory for the three manifests")
    args = parser.parse_args()
    try:
        prepare(args.archive, args.output)
    except (OSError, ValueError, zipfile.BadZipFile) as error:
        parser.error(str(error))
    print(f"Prepared local manifests in {args.output}. Windows installation checks are still required.")
