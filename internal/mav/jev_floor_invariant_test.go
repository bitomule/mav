package mav

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Why this test exists, and it is not a hypothetical.
//
// askJevChoice in uifind_jev.go says, in a comment: "The floor is checked here
// and nowhere else: this is the one path that spawns jevi." That was true when
// it was written, and it STOPPED BEING TRUE IN SILENCE — a branch added a
// second place that built its own `jevi` command and never went through
// checkJevVersion. Nothing failed, because nothing was checking.
//
// What that costs, under the floor: jevi gained the off-menu guard in 0.4.0
// (`off_menu_answer`), and mav's own equivalent guard was DELETED in the same
// change that introduced the floor, precisely because jevi now owned it. So on
// an older jevi, on a path that skipped the floor, an answer could name an
// element that was never in the batch it was offered — including which value to
// type into it. That branch was closed, so the hole is not in the tree today.
// What was still missing is anything stopping it coming back.
//
// A comment asserting an invariant does not maintain it. This does.
//
// How it decides, and what it therefore cannot see: it reads the package's own
// non-test sources and looks for a process-spawning call that carries the
// literal string "jevi". Every such call must sit in a function that also calls
// checkJevVersion, earlier in that same function. Building the program name out
// of a variable would slip past this, which is the honest limit of a source
// scan; spelling "jevi" at the call is how every site in this package does it,
// and the failure message says so.
func TestEveryPathThatSpawnsJeviChecksTheVersionFloor(t *testing.T) {
	// The one exemption, and it is the floor itself: readJevVersion IS the
	// `jevi --version` call checkJevVersion makes. Requiring it to call the
	// check would be requiring it to call itself.
	exempt := map[string]string{
		"readJevVersion": "it is the floor check: this call is `jevi --version`",
	}

	// Callees that start a process. A literal "jevi" passed to anything else
	// (a tool-presence lookup, an error message) is not a spawn.
	spawners := map[string]bool{
		"Command": true, "CommandContext": true,
		"Start": true, "Run": true, "Output": true, "CombinedOutput": true,
	}

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	spawnSites := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			spawns := jeviSpawnPositions(fn, spawners)
			if len(spawns) == 0 {
				continue
			}
			spawnSites += len(spawns)
			if why, ok := exempt[fn.Name.Name]; ok {
				t.Logf("%s: %d jevi spawn(s), exempt because %s", fn.Name.Name, len(spawns), why)
				continue
			}
			floor := firstCallTo(fn, "checkJevVersion")
			for _, at := range spawns {
				switch {
				case floor == token.NoPos:
					t.Errorf("%s spawns jevi at %s without calling checkJevVersion.\n"+
						"Every path that spawns jevi must pass the version floor first: below %s, "+
						"jevi does not refuse an answer naming something it never offered, and mav "+
						"stopped checking that itself.",
						fn.Name.Name, fset.Position(at), jevMinVersion)
				case floor > at:
					t.Errorf("%s spawns jevi at %s but calls checkJevVersion only afterwards, at %s; "+
						"the floor has to hold before the question is put",
						fn.Name.Name, fset.Position(at), fset.Position(floor))
				}
			}
		}
	}

	// Without this the test passes loudly when it has stopped looking at
	// anything -- a rename of the spawn helper, a move to another package, a
	// scan that quietly matches nothing. Two sites are known today:
	// askJevChoice and readJevVersion.
	if spawnSites < 2 {
		t.Fatalf("found %d jevi spawn site(s) in this package; expected at least the two known ones "+
			"(askJevChoice and readJevVersion). If jevi is now spawned somewhere this scan cannot "+
			"see -- another package, or a command name built from a variable -- this test is no "+
			"longer guarding anything and has to be rewritten, not deleted", spawnSites)
	}
}

// jeviSpawnPositions returns the position of every call inside fn that starts a
// process and names "jevi" as a literal argument.
func jeviSpawnPositions(fn *ast.FuncDecl, spawners map[string]bool) []token.Pos {
	var out []token.Pos
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !spawners[calleeName(call.Fun)] {
			return true
		}
		for _, arg := range call.Args {
			lit, ok := arg.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			if v, err := strconv.Unquote(lit.Value); err == nil && v == "jevi" {
				out = append(out, call.Lparen)
				return true
			}
		}
		return true
	})
	return out
}

// firstCallTo returns the position of the first call to name inside fn, or
// token.NoPos when there is none.
func firstCallTo(fn *ast.FuncDecl, name string) token.Pos {
	found := token.NoPos
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found != token.NoPos {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok && calleeName(call.Fun) == name {
			found = call.Lparen
			return false
		}
		return true
	})
	return found
}

func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}
