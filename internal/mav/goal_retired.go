package mav

import "strings"

// The retired command is rejected before configuration, VM attachment, target
// selection, model calls or input. The suggested command is data, never run.
func (c CLI) removedGoto(opts GlobalOptions, args []string) error {
	return Fail("goto_removed", map[string]string{
		"next":    "goto was removed in MAV 0.35.0; use goal. Replace --arrived-when with --succeeded-when; YAML goto with goal and arrivedWhen with succeededWhen",
		"command": gotoMigrationCommand(opts, args),
	}).Write(c.Stdout)
}

func gotoMigrationCommand(opts GlobalOptions, args []string) string {
	parts := []string{"mav", "goal", "--mode", "navigation"}
	if opts.Help {
		return "mav goal --help"
	}
	if len(args) == 0 {
		args = []string{"<objective>"}
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--arrived-when":
			a = "--succeeded-when"
		case strings.HasPrefix(a, "--arrived-when="):
			a = "--succeeded-when=" + strings.TrimPrefix(a, "--arrived-when=")
		}
		parts = append(parts, shellQuote(a))
		// Values are literals, even when they look like another option.
		if a == "--succeeded-when" || a == "--max-steps" || a == "--timeout" || a == "--dismiss-permission" {
			if i+1 < len(args) {
				i++
				parts = append(parts, shellQuote(args[i]))
			}
		}
	}
	if opts.PreferDriver != "" {
		parts = append(parts, "--prefer-driver", shellQuote(opts.PreferDriver))
	}
	if opts.Profile != "" {
		parts = append(parts, "--profile", shellQuote(opts.Profile))
	}
	if opts.Verbose {
		parts = append(parts, "--verbose")
	}
	if opts.Raw {
		parts = append(parts, "--raw")
	}
	return strings.Join(parts, " ")
}
