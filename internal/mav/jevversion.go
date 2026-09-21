package mav

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// The floor under the jevi we are willing to ask, and the reason it exists is a
// guard that USED to live here and no longer does.
//
// mav checked every answer against the menu it had offered: a label naming
// something that was not in the batch was discarded rather than looked up. jevi
// 0.4.0 does that itself — `off_menu_answer` — and, crucially, it WITHHOLDS the
// label rather than handing it over with a warning attached, so an answer mav
// cannot vouch for never reaches mav at all. Two checks for one rule is one
// place too many, so mav's was removed.
//
// But removing a guard because a dependency now has it is only safe while that
// dependency is actually the one installed. mav shells out to whatever `jevi` is
// on PATH and pins nothing, so an older binary would quietly reopen the hole
// with no error anywhere — and "the version without the check" is not a
// hypothetical, it is what everyone had until 0.4.0 shipped. Hence a floor, and
// hence it is stated as a version rather than as trust.
const jevMinVersion = "0.4.0"

// jevVersionOnce makes this cost ONCE PER PROCESS, and that is not tidiness.
//
// Measured in this repo and written up in docs/design/goto.md §10:
// `resolveCapabilities` cost 310 ms, of which **192 ms was a single
// `idb --version`** — idb is Python and starting it is 116 ms — and it was paid
// by every command for a hint that is only read after something has already
// failed. Making it lazy took 157 ms off every tap.
//
// A `jevi --version` per model call would repeat that mistake exactly, in the
// loop this whole command exists to keep fast. So: once, behind a sync.Once, on
// the path that was going to spawn jevi anyway. Whoever adds the next version
// floor should copy this and not the obvious thing.
var (
	jevVersionOnce sync.Once
	jevVersionErr  error
)

// checkJevVersion returns nil when the installed jevi is new enough, and the
// same error on every subsequent call without spawning anything again.
//
// It is called only from the path that asks jev a question. Someone driving a
// simulator without `find` or `goto` never pays it and is never stopped by it:
// a floor under a tool you are not using is noise.
func checkJevVersion(ctx context.Context) error {
	jevVersionOnce.Do(func() {
		jevVersionErr = readJevVersion(ctx)
	})
	return jevVersionErr
}

func readJevVersion(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "jevi", "--version").Output()
	if err != nil {
		// Not reachable, not readable, not there. That is "could not ask",
		// which the caller already distinguishes from "asked and unsure"; the
		// floor has nothing to add to it.
		return nil
	}
	got := parseJevVersion(string(out))
	if got == "" {
		// A version we cannot parse is not a version we can refuse on. Saying
		// nothing is better than blocking a working install over a string.
		return nil
	}
	if versionAtLeast(got, jevMinVersion) {
		return nil
	}
	return fmt.Errorf("jevi %s is installed and mav needs %s or newer: "+
		"since %s jevi refuses an answer that names something it never offered, "+
		"and mav stopped checking that itself. Update with "+
		"`cargo install jevi --locked --force`",
		got, jevMinVersion, jevMinVersion)
}

// parseJevVersion reads the number out of `jevi --version`, which prints
// "jevi 0.4.0".
func parseJevVersion(out string) string {
	fields := strings.Fields(strings.TrimSpace(out))
	for i := len(fields) - 1; i >= 0; i-- {
		if f := strings.TrimPrefix(fields[i], "v"); len(f) > 0 && f[0] >= '0' && f[0] <= '9' {
			return f
		}
	}
	return ""
}

// versionAtLeast compares dotted numbers left to right. A table rather than a
// semver dependency because the set is small and closed, the same reasoning
// foldAccent is built on.
func versionAtLeast(got, want string) bool {
	g, w := versionParts(got), versionParts(want)
	for i := 0; i < len(w); i++ {
		var gi int
		if i < len(g) {
			gi = g[i]
		}
		if gi != w[i] {
			return gi > w[i]
		}
	}
	return true
}

func versionParts(v string) []int {
	// A pre-release suffix is dropped rather than ordered: "0.4.0-rc1" reads as
	// 0.4.0 here. Refusing a release candidate of the version that carries the
	// fix would stop someone testing it, and that is the wrong way round.
	if cut := strings.IndexAny(v, "-+"); cut >= 0 {
		v = v[:cut]
	}
	fields := strings.Split(v, ".")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}
