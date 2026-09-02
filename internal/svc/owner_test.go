// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassify(t *testing.T) {
	dir := t.TempDir()

	missing := filepath.Join(dir, "missing.plist")
	own, err := Classify(missing)
	if err != nil {
		t.Fatal(err)
	}
	if own != OwnershipMissing {
		t.Fatalf("missing: %s", own)
	}

	ours := filepath.Join(dir, "ours.plist")
	if err := os.WriteFile(ours, []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"+MarkerPlist+"\n<plist/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	own, err = Classify(ours)
	if err != nil {
		t.Fatal(err)
	}
	if own != OwnershipOurs {
		t.Fatalf("ours: %s", own)
	}

	unit := filepath.Join(dir, "hister.service")
	if err := os.WriteFile(unit, []byte(MarkerSystemd+"\n[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	own, err = Classify(unit)
	if err != nil {
		t.Fatal(err)
	}
	if own != OwnershipOurs {
		t.Fatalf("systemd ours: %s", own)
	}

	foreign := filepath.Join(dir, "foreign.service")
	if err := os.WriteFile(foreign, []byte("[Service]\nExecStart=/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	own, err = Classify(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if own != OwnershipForeign {
		t.Fatalf("no marker: %s", own)
	}

	link := filepath.Join(dir, "link.service")
	if err := os.Symlink(unit, link); err != nil {
		t.Fatal(err)
	}
	own, err = Classify(link)
	if err != nil {
		t.Fatal(err)
	}
	if own != OwnershipForeign {
		t.Fatalf("symlink: %s", own)
	}

	embedded := filepath.Join(dir, "embedded.service")
	if err := os.WriteFile(embedded, []byte("[Unit]\n# example: "+MarkerSystemd+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	own, err = Classify(embedded)
	if err != nil {
		t.Fatal(err)
	}
	if own != OwnershipForeign {
		t.Fatalf("embedded marker: %s", own)
	}
}

func TestClassifyEnableLink(t *testing.T) {
	dir := t.TempDir()
	unit := filepath.Join(dir, "hister.service")
	wants := filepath.Join(dir, "hister.service.wants")

	own, err := classifyEnableLink(wants, unit)
	if err != nil {
		t.Fatal(err)
	}
	if own != OwnershipMissing {
		t.Fatalf("missing wants: %s", own)
	}

	if err := os.Symlink(unit, wants); err != nil {
		t.Fatal(err)
	}
	own, err = classifyEnableLink(wants, unit)
	if err != nil {
		t.Fatal(err)
	}
	if own != OwnershipOurs {
		t.Fatalf("ours wants: %s", own)
	}

	if err := os.Remove(wants); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wants, []byte("not a symlink"), 0o644); err != nil {
		t.Fatal(err)
	}
	own, err = classifyEnableLink(wants, unit)
	if err != nil {
		t.Fatal(err)
	}
	if own != OwnershipForeign {
		t.Fatalf("regular file wants: %s", own)
	}

	if err := os.Remove(wants); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other.service")
	if err := os.Symlink(other, wants); err != nil {
		t.Fatal(err)
	}
	own, err = classifyEnableLink(wants, unit)
	if err != nil {
		t.Fatal(err)
	}
	if own != OwnershipForeign {
		t.Fatalf("wrong target wants: %s", own)
	}

	if err := os.Remove(wants); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(wants, 0o755); err != nil {
		t.Fatal(err)
	}
	own, err = classifyEnableLink(wants, unit)
	if err != nil {
		t.Fatal(err)
	}
	if own != OwnershipForeign {
		t.Fatalf("directory wants: %s", own)
	}
}
