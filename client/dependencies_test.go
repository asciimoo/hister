package client

import (
	"os/exec"
	"strings"
	"testing"
)

func TestClientDependencies(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "github.com/asciimoo/nogon/client")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list client dependencies: %v\n%s", err, out)
	}
	var unexpected []string
	for pkg := range strings.FieldsSeq(string(out)) {
		switch pkg {
		case "github.com/asciimoo/nogon/client", "github.com/asciimoo/nogon/server/types":
		default:
			unexpected = append(unexpected, pkg)
		}
	}
	if len(unexpected) != 0 {
		t.Fatalf("client must depend only on the standard library and API types; found %d other packages, including %s", len(unexpected), unexpected[0])
	}
}
