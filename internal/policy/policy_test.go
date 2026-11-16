package policy

import (
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"
)

// Anti-Freeze never uses the network (section 6). These checks run on the
// import graph:
//
//  1. No package in this module imports a networking package or os/exec
//     directly (build scripts under scripts/ may run fixed system tools).
//  2. Nothing under internal/ depends on an HTTP, TLS, mail or RPC client,
//     even indirectly. (SQLite's driver pulls in net/url for parsing its
//     connection string and a UUID package that imports net; neither opens
//     a connection.)
//
// The Wails layer in cmd/anti-freeze serves the UI through net/http handler
// types over the in-process wails:// scheme, never a port; that is checked
// at runtime instead (scripts/measure.sh reports open sockets).

type pkg struct {
	ImportPath string
	Imports    []string
	Deps       []string
	Module     *struct{ Path string }
}

func list(t *testing.T, patterns ...string) []pkg {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not available")
	}
	args := append([]string{"list", "-tags", "production", "-deps", "-json"}, patterns...)
	cmd := exec.Command("go", args...) // fixed tool and arguments
	cmd.Dir = "../.."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var pkgs []pkg
	for {
		var p pkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

const module = "github.com/Prime-Cave/anti-freeze"

var forbiddenDirect = map[string]bool{
	"net": true, "net/http": true, "net/rpc": true, "net/smtp": true, "crypto/tls": true,
	"net/http/httptest": true, "os/exec": true,
}

func TestNoNetworkImportsInOurCode(t *testing.T) {
	seen := 0
	for _, p := range list(t, "./...") {
		if p.Module == nil || p.Module.Path != module {
			continue
		}
		seen++
		if strings.HasPrefix(p.ImportPath, module+"/scripts/") {
			continue
		}
		for _, imp := range p.Imports {
			if forbiddenDirect[imp] {
				t.Errorf("%s imports %s", p.ImportPath, imp)
			}
		}
	}
	if seen < 8 {
		t.Fatalf("only %d packages of this module were checked", seen)
	}
}

func TestNoNetworkClientsUnderInternal(t *testing.T) {
	for _, p := range list(t, "./internal/...") {
		switch p.ImportPath {
		case "net/http", "crypto/tls", "net/smtp", "net/rpc", "net/http/httptrace":
			t.Errorf("internal/ depends on %s", p.ImportPath)
		}
	}
}
