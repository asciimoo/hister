// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"strings"
	"testing"
)

func testDef(pathExtra string) Definition {
	return Definition{
		Binary:     "/usr/local/bin/hister" + pathExtra,
		ConfigPath: "/home/user/My Files/config.yml",
		DataDir:    "/home/user/My Files/data",
		StdoutLog:  "/home/user/Library/Logs/hister.log",
		StderrLog:  "/home/user/Library/Logs/hister-error.log",
	}
}

func TestRenderLaunchdEscapesAndKeepAlive(t *testing.T) {
	xml, err := RenderLaunchd(testDef(" & bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		MarkerPlist,
		"<string>org.hister.server</string>",
		"<string>/usr/local/bin/hister &amp; bin</string>",
		"<string>listen</string>",
		"<string>--config</string>",
		"<string>/home/user/My Files/config.yml</string>",
		"<key>HISTER_DATA_DIR</key>",
		"<key>Crashed</key>",
		"<true/>",
		"<key>SuccessfulExit</key>",
		"<false/>",
		"<key>RunAtLoad</key>",
		"<integer>63</integer>",
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("plist missing %q\n%s", want, xml)
		}
	}
	if strings.Contains(xml, "After=") || strings.Contains(strings.ToLower(xml), "--log-level") {
		t.Fatal("plist contains unexpected keys")
	}
}

func TestRenderSystemdUserQuotesAndMarker(t *testing.T) {
	unit, err := RenderSystemdUser(testDef(""))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		MarkerSystemd,
		`ExecStart="/usr/local/bin/hister" "listen" "--config" "/home/user/My Files/config.yml"`,
		`WorkingDirectory=/home/user/My Files/data`,
		`Environment="HISTER_DATA_DIR=/home/user/My Files/data"`,
		"Restart=on-failure",
		"UMask=0077",
		"WantedBy=default.target",
		"Type=exec",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "After=network.target") {
		t.Fatal("user unit must not wait on network.target")
	}
	if strings.Contains(unit, "--log-level") {
		t.Fatal("unit must not persist --log-level")
	}
}

func TestRenderSystemdUserEscapesSpecials(t *testing.T) {
	def := Definition{
		Binary:  `/tmp/his"ter`,
		DataDir: `/tmp/data$dir`,
	}
	unit, err := RenderSystemdUser(def)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, `ExecStart="/tmp/his\"ter" "listen"`) {
		t.Fatalf("quoted binary:\n%s", unit)
	}
	if strings.Contains(unit, `ExecStart=`) && strings.Contains(unit, `\$`) {
		t.Fatalf("ExecStart must not use backslash-dollar:\n%s", unit)
	}
	if !strings.Contains(unit, `Environment="HISTER_DATA_DIR=/tmp/data$dir"`) {
		t.Fatalf("Environment $:\n%s", unit)
	}
	if !strings.Contains(unit, `WorkingDirectory=/tmp/data$dir`) {
		t.Fatalf("WorkingDirectory $:\n%s", unit)
	}
}

func TestRenderSystemdUserExecStartDoublesDollar(t *testing.T) {
	def := Definition{
		Binary:  `/tmp/his$ter`,
		DataDir: `/tmp/data`,
	}
	unit, err := RenderSystemdUser(def)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, `ExecStart="/tmp/his$$ter" "listen"`) {
		t.Fatalf("ExecStart $$:\n%s", unit)
	}
}

func TestRenderSystemdUserRejectsControlChars(t *testing.T) {
	def := Definition{
		Binary:  "/tmp/his\nter",
		DataDir: "/tmp/data",
	}
	if _, err := RenderSystemdUser(def); err == nil {
		t.Fatal("expected control character error")
	}
}

func TestRenderRejectsRelativePaths(t *testing.T) {
	if _, err := RenderLaunchd(Definition{Binary: "hister", DataDir: "/tmp/data"}); err == nil {
		t.Fatal("expected relative binary to fail")
	}
	if _, err := RenderSystemdUser(Definition{Binary: "/bin/hister", DataDir: "data"}); err == nil {
		t.Fatal("expected relative data dir to fail")
	}
}

func TestValidateRejectsInvalidUTF8(t *testing.T) {
	def := Definition{
		Binary:  "/tmp/" + string([]byte{0xff, 0xfe}) + "hister",
		DataDir: "/tmp/data",
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected invalid UTF-8 error")
	}
	if _, err := RenderLaunchd(def); err == nil {
		t.Fatal("expected launchd render to reject invalid UTF-8")
	}
}
