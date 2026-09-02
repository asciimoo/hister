// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"strings"
)

func RenderSystemdUser(def Definition) (string, error) {
	if err := def.Validate(); err != nil {
		return "", err
	}

	execParts := make([]string, 0, 4)
	for _, arg := range def.Args() {
		quoted, err := quoteSystemdExecArg(arg)
		if err != nil {
			return "", err
		}
		execParts = append(execParts, quoted)
	}
	workDir, err := quoteSystemdPath(def.DataDir)
	if err != nil {
		return "", err
	}
	env, err := quoteSystemdValue("HISTER_DATA_DIR=" + def.DataDir)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(MarkerSystemd + "\n")
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Hister search engine\n")
	b.WriteString("\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=exec\n")
	b.WriteString("ExecStart=" + strings.Join(execParts, " ") + "\n")
	b.WriteString("WorkingDirectory=" + workDir + "\n")
	b.WriteString("Environment=" + env + "\n")
	b.WriteString("Restart=on-failure\n")
	b.WriteString("UMask=0077\n")
	b.WriteString("\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return b.String(), nil
}

func quoteSystemdExecArg(s string) (string, error) {
	if err := rejectPersistedControlChars(s); err != nil {
		return "", err
	}
	s = strings.ReplaceAll(s, "%", "%%")
	s = strings.ReplaceAll(s, "$", "$$")
	return quoteSystemdToken(s), nil
}

func quoteSystemdValue(s string) (string, error) {
	if err := rejectPersistedControlChars(s); err != nil {
		return "", err
	}
	s = strings.ReplaceAll(s, "%", "%%")
	return quoteSystemdToken(s), nil
}

func quoteSystemdPath(s string) (string, error) {
	if err := rejectPersistedControlChars(s); err != nil {
		return "", err
	}
	return strings.ReplaceAll(s, "%", "%%"), nil
}

func quoteSystemdToken(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if r == '\\' || r == '"' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}
