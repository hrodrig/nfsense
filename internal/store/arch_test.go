package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The rule this test enforces, stated once so later phases extend it rather
// than reinterpret it.
//
// This file lives in internal/store because that is where the validation map
// put it, but it asserts about the WHOLE MODULE: it reads the module's
// dependency graph with `go list -deps ./...` and fails on any firewall
// capability reachable from it (W8/T-1-05).
//
// (1) At every first-party import edge, these are banned:
//
//	"os/exec"                     shelling out is how an apply path starts
//	                              (Phase 2 will need it once, behind
//	                              FirewallBackend — until then, nothing here)
//	"github.com/google/nftables"  direct nftables over netlink
//	"sigs.k8s.io/knftables"       nftables over netlink, K8s-scoped
//	any path containing "netlink" vishvananda/netlink, netns, ...
//	any path containing "/net/"   module net packages that exist only to get
//	                              at the kernel
//
// (2) Anywhere in the dependency graph — including through a third-party
// package — the nftables/netlink family above is banned: linking one of those
// packages IS the capability.
//
// Bare "net", "net/netip", "net/url" and "net/http" are allowed: none of them
// contains the substring "/net/" (there is no leading slash in "net/ip"), and
// none of them can open a netlink socket on its own.
//
// "os/exec" is deliberately NOT enforced graph-wide, because that is not
// achievable and never was: `go list -deps ./...` on the very first commit of
// this module already contains os/exec, pulled in by modernc.org/libc — the
// pure-Go SQLite driver this project chose in its stack decision. The
// meaningful claim, and the one asserted here, is that no nfsense package
// shells out. When that driver changes, this comment is the place to revisit
// it, not a new interpretation of the test.
//
// Test-only imports are ignored by construction: only List-Imports of
// first-party production packages are inspected, so restart_test.go may import
// os/exec to cross a real process boundary without tripping this gate. That is
// the whole reason the check is written against first-party edges instead of
// grepping every file.

// vetoedFirstParty lists imports no nfsense package may declare, with the
// reason shown in the failure message.
var vetoedFirstParty = map[string]string{
	"os/exec":                        "shelling out: the apply path this phase is proving absent",
	"github.com/google/nftables":     "direct nftables over netlink",
	"sigs.k8s.io/knftables":          "nftables over netlink (K8s-scoped)",
	"github.com/vishvananda/netlink": "netlink",
	"github.com/vishvananda/netns":   "network namespaces via netlink",
}

// TestNoBannedImports reads the module's dependency graph and fails if any
// package that reaches the kernel's packet filter — or shells out to reach it —
// is imported by nfsense, or is linked at all.
func TestNoBannedImports(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		// A gate that cannot run must say so out loud. Skipping is visible in
		// `go test -v` output and in CI logs, unlike a silent pass.
		t.Skip("go toolchain not on PATH: the dependency graph cannot be read, so this gate cannot speak")
	}

	root := moduleRoot(t)
	modulePath := mainModulePath(t, root)
	pkgs := listDeps(t, root)

	findings := map[string]string{} // "importer imports pkg" -> reason
	record := func(importer, pkg, reason string) {
		findings[fmt.Sprintf("%s imports %q", importer, pkg)] = reason
	}

	// (1) Capability anywhere in the graph, whoever drags it in.
	for _, p := range pkgs {
		if reason, bad := capabilityReason(p.ImportPath); bad {
			for _, importer := range reverseImporters(pkgs, p.ImportPath) {
				record(importer, p.ImportPath, reason)
			}
		}
	}

	// (2) First-party direct import edges.
	for _, p := range pkgs {
		if p.ForTest != "" || !isFirstParty(p.ImportPath, modulePath) {
			continue
		}
		for _, imp := range p.Imports {
			reason, bad := capabilityReason(imp)
			if !bad {
				reason, bad = shellOutReason(imp)
			}
			if bad {
				record(p.ImportPath, imp, reason)
			}
		}
	}

	t.Logf("checked %d packages reachable from %s", len(pkgs), modulePath)

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for what, reason := range findings {
		lines = append(lines, fmt.Sprintf("  %s (%s)", what, reason))
	}
	sort.Strings(lines)
	t.Fatalf("banned import found in the module's dependency graph:\n%s",
		strings.Join(lines, "\n"))
}

// capabilityReason reports why linking importPath at all would hand the binary
// the ability to reach the kernel's packet filter.
func capabilityReason(importPath string) (string, bool) {
	for banned, reason := range map[string]string{
		"github.com/google/nftables":     "direct nftables over netlink",
		"sigs.k8s.io/knftables":          "nftables over netlink (K8s-scoped)",
		"github.com/vishvananda/netlink": "netlink",
		"github.com/vishvananda/netns":   "network namespaces via netlink",
	} {
		if importPath == banned || strings.HasPrefix(importPath, banned+"/") {
			return reason, true
		}
	}
	if strings.Contains(importPath, "netlink") {
		return "netlink access", true
	}
	return "", false
}

// shellOutReason reports why a first-party package may not import importPath.
// These are the edges that are legitimate in someone else's code (the stdlib
// and the SQLite driver both use os/exec) and not legitimate here.
func shellOutReason(importPath string) (string, bool) {
	if reason, bad := vetoedFirstParty[importPath]; bad {
		return reason, true
	}
	for banned, reason := range vetoedFirstParty {
		if strings.HasPrefix(importPath, banned+"/") {
			return reason, true
		}
	}
	if strings.Contains(importPath, "/net/") {
		return "module net package (only reachable to get at the kernel); bare net and net/netip are allowed", true
	}
	return "", false
}

// listedPackage is the subset of `go list -json` this test needs.
type listedPackage struct {
	ImportPath string
	Imports    []string
	ForTest    string
}

// listDeps returns every package in the module's dependency graph. -json is
// used rather than the bare list only so a failure can name the importer as
// well as the import, which is what makes the message actionable.
func listDeps(t *testing.T, root string) []listedPackage {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-json", "./...")
	cmd.Dir = root
	cmd.Env = os.Environ()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps ./... in %s: %v\n%s", root, err, stderr.String())
	}

	dec := json.NewDecoder(bytes.NewReader(out))
	var pkgs []listedPackage
	for {
		var p listedPackage
		err := dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode go list output: %v\n%s", err, stderr.String())
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) == 0 {
		t.Fatalf("go list -deps ./... in %s returned no packages", root)
	}
	return pkgs
}

// moduleRoot resolves the module directory from the toolchain itself, so the
// test does not depend on the working directory `go test` happens to use.
func moduleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		t.Fatalf("go env GOMOD = %q: not inside a Go module", gomod)
	}
	return filepath.Dir(gomod)
}

// mainModulePath is the module's own import prefix, used to tell first-party
// packages from dependencies.
func mainModulePath(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-m")
	cmd.Dir = root
	cmd.Env = os.Environ()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m in %s: %v\n%s", root, err, stderr.String())
	}
	modulePath := strings.TrimSpace(string(out))
	if modulePath == "" {
		t.Fatalf("go list -m in %s printed nothing", root)
	}
	return modulePath
}

func isFirstParty(importPath, modulePath string) bool {
	return importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/")
}

// reverseImporters names every package in the graph that imports target.
func reverseImporters(pkgs []listedPackage, target string) []string {
	var importers []string
	for _, p := range pkgs {
		if p.ForTest != "" {
			continue
		}
		for _, imp := range p.Imports {
			if imp == target {
				importers = append(importers, p.ImportPath)
				break
			}
		}
	}
	return importers
}
