package drivers

import (
	"strconv"
	"strings"
)

// VersionAtLeast compares dotted numbers left to right. A table rather than a
// semver dependency because the set is small and closed.
func VersionAtLeast(got, want string) bool {
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
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if cut := strings.IndexAny(v, "-+_"); cut >= 0 {
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
