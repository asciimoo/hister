// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"encoding/xml"
	"strconv"
	"strings"
)

const launchdLabel = "org.hister.server"

func RenderLaunchd(def Definition) (string, error) {
	if err := def.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(MarkerPlist + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	b.WriteString("<dict>\n")
	if err := writePlistKey(&b, "Label"); err != nil {
		return "", err
	}
	if err := writePlistString(&b, launchdLabel); err != nil {
		return "", err
	}

	if err := writePlistKey(&b, "ProgramArguments"); err != nil {
		return "", err
	}
	b.WriteString("<array>\n")
	for _, arg := range def.Args() {
		if err := writePlistString(&b, arg); err != nil {
			return "", err
		}
	}
	b.WriteString("</array>\n")

	if err := writePlistKey(&b, "WorkingDirectory"); err != nil {
		return "", err
	}
	if err := writePlistString(&b, def.DataDir); err != nil {
		return "", err
	}

	if err := writePlistKey(&b, "EnvironmentVariables"); err != nil {
		return "", err
	}
	b.WriteString("<dict>\n")
	if err := writePlistKey(&b, "HISTER_DATA_DIR"); err != nil {
		return "", err
	}
	if err := writePlistString(&b, def.DataDir); err != nil {
		return "", err
	}
	b.WriteString("</dict>\n")

	if err := writePlistKey(&b, "RunAtLoad"); err != nil {
		return "", err
	}
	b.WriteString("<true/>\n")

	if err := writePlistKey(&b, "KeepAlive"); err != nil {
		return "", err
	}
	b.WriteString("<dict>\n")
	if err := writePlistKey(&b, "Crashed"); err != nil {
		return "", err
	}
	b.WriteString("<true/>\n")
	if err := writePlistKey(&b, "SuccessfulExit"); err != nil {
		return "", err
	}
	b.WriteString("<false/>\n")
	b.WriteString("</dict>\n")

	if err := writePlistKey(&b, "ProcessType"); err != nil {
		return "", err
	}
	if err := writePlistString(&b, "Background"); err != nil {
		return "", err
	}

	if err := writePlistKey(&b, "Umask"); err != nil {
		return "", err
	}
	b.WriteString("<integer>")
	b.WriteString(strconv.Itoa(0o077))
	b.WriteString("</integer>\n")

	if def.StdoutLog != "" {
		if err := writePlistKey(&b, "StandardOutPath"); err != nil {
			return "", err
		}
		if err := writePlistString(&b, def.StdoutLog); err != nil {
			return "", err
		}
	}
	if def.StderrLog != "" {
		if err := writePlistKey(&b, "StandardErrorPath"); err != nil {
			return "", err
		}
		if err := writePlistString(&b, def.StderrLog); err != nil {
			return "", err
		}
	}

	b.WriteString("</dict>\n</plist>\n")
	return b.String(), nil
}

func writePlistKey(b *strings.Builder, key string) error {
	escaped, err := xmlEscape(key)
	if err != nil {
		return err
	}
	b.WriteString("<key>")
	b.WriteString(escaped)
	b.WriteString("</key>\n")
	return nil
}

func writePlistString(b *strings.Builder, s string) error {
	escaped, err := xmlEscape(s)
	if err != nil {
		return err
	}
	b.WriteString("<string>")
	b.WriteString(escaped)
	b.WriteString("</string>\n")
	return nil
}

func xmlEscape(s string) (string, error) {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return "", err
	}
	return b.String(), nil
}
