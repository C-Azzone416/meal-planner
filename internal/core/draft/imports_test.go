package draft_test

import (
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/C-Azzone416/meal-planner"

// forbidden lists standard-library packages that domain code must never depend on, directly or
// transitively: networking, HTTP, TLS, SQL, process execution and plugin loading. (net/url is
// fine: it only parses text, and it is how source URLs are checked without ever being fetched.)
var forbidden = []string{"net", "net/http", "crypto/tls", "database/sql", "os/exec", "plugin", "C"}

func isForbidden(imp string) bool {
	for _, f := range forbidden {
		if imp == f || strings.HasPrefix(imp, f+"/") && f != "net" { // "net/url" is allowed, "net/http..." is listed
			return true
		}
	}
	return false
}

var packages = []string{"", "/memstore", "/storetest"}

func goList(t *testing.T, format, pkg string, deps bool) []string {
	t.Helper()
	args := []string{"list", "-f", format}
	if deps {
		args = append(args, "-deps")
	}
	out, err := exec.Command("go", append(args, modulePath+"/internal/core/draft"+pkg)...).CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// The core, its in-memory store and the conformance suite depend on the standard library only (plus
// each other): no MCP SDK, CLI/flag code, HTTP, database, provider SDK, process execution, or
// concrete store such as internal/localstore.
func TestNoForbiddenDependencies(t *testing.T) {
	for _, pkg := range packages {
		for _, line := range goList(t, "{{.ImportPath}} {{.Standard}}", pkg, true) {
			f := strings.Fields(line)
			if len(f) != 2 {
				t.Fatalf("unexpected go list line %q", line)
			}
			imp, std := f[0], f[1] == "true"
			switch {
			case !std && !strings.HasPrefix(imp, modulePath+"/internal/core/draft"):
				t.Errorf("internal/core/draft%s depends on %s: only the standard library and internal/core/draft/... are allowed", pkg, imp)
			case std && isForbidden(imp):
				t.Errorf("internal/core/draft%s depends on forbidden package %s", pkg, imp)
			}
		}
	}
}

// The same holds for what each package imports itself. Only this file's own test may run `go list`.
func TestNoForbiddenDirectImports(t *testing.T) {
	for _, pkg := range packages {
		for kind, format := range map[string]string{
			"production": `{{join .Imports "\n"}}`,
			"test":       `{{join .TestImports "\n"}}{{"\n"}}{{join .XTestImports "\n"}}`,
		} {
			for _, imp := range goList(t, format, pkg, false) {
				if imp == "" {
					continue
				}
				if isForbidden(imp) && !(kind == "test" && pkg == "" && imp == "os/exec") {
					t.Errorf("internal/core/draft%s %s code imports forbidden package %s", pkg, kind, imp)
				}
				if strings.Contains(strings.SplitN(imp, "/", 2)[0], ".") && !strings.HasPrefix(imp, modulePath+"/internal/core/draft") {
					t.Errorf("internal/core/draft%s %s code imports non-standard package %s", pkg, kind, imp)
				}
			}
		}
	}
}

func TestIsForbidden(t *testing.T) {
	for imp, want := range map[string]bool{
		"net": true, "net/http": true, "net/http/httputil": true, "crypto/tls": true, "database/sql": true, "database/sql/driver": true,
		"os/exec": true, "plugin": true, "C": true,
		"net/url": false, "net/netip": false, "os": false, "encoding/json": false, "time": false, "context": false, "crypto/rand": false,
	} {
		if got := isForbidden(imp); got != want {
			t.Errorf("isForbidden(%q) = %v, want %v", imp, got, want)
		}
	}
}
