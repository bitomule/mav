package mav

// Version is what this binary reports as itself. It is stamped at link time
// by the release build (-X github.com/bitomule/mav/internal/mav.Version) and
// left as "dev" otherwise, so a locally built binary never claims to be a
// release it is not.
//
// It exists because until now mav could not answer the question. An agent
// reporting a bug, a run whose evidence is read weeks later, and anyone
// comparing what is installed against what a project expects all need a
// version, and `mav --version` answered `unknown_command`.
var Version = "dev"
