package mav

import (
	"context"
	"io"
	"strconv"
)

// `mav jev` manages the credential `mav ui find` needs, and says where the one
// in use came from. Both halves exist because the alternative to printing the
// source is remembering what you configured.
func (c CLI) jev(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("jev_subcommand_missing", map[string]string{
			"usage": "mav jev set-key < key.txt | mav jev doctor",
		}).Write(c.Stdout)
	}
	switch args[0] {
	case "set-key":
		return c.jevSetKey()
	case "doctor":
		return c.jevDoctor()
	}
	return Fail("jev_subcommand_unknown", map[string]string{
		"usage": "mav jev set-key < key.txt | mav jev doctor",
	}).Write(c.Stdout)
}

// jevSetKey reads the key from stdin rather than an argument, so the secret
// never reaches shell history or a process listing.
func (c CLI) jevSetKey() error {
	raw, err := io.ReadAll(c.Stdin)
	if err != nil {
		return Fail("jev_key_unreadable", map[string]string{"detail": err.Error()}).Write(c.Stdout)
	}
	where, err := StoreJevKey(string(raw))
	if err != nil {
		return Fail("jev_key_not_stored", map[string]string{
			"detail": err.Error(),
			"usage":  "mav jev set-key < key.txt",
		}).Write(c.Stdout)
	}
	return c.OK("jev.set-key", map[string]string{"stored_in": where}).Write(c.Stdout)
}

// jevDoctor says whether there is a key and which of the three places it came
// from, without ever printing the key.
func (c CLI) jevDoctor() error {
	fields := map[string]string{"config_path": JevKeyConfigPath()}
	if refused := findIsRefusedHere(); refused != "" {
		fields["refused"] = refused
	}
	_, source, ok := ResolveJevKey()
	fields["key"] = strconv.FormatBool(ok)
	if ok {
		fields["source"] = source.Describe()
	} else {
		fields["next"] = MissingJevKeyNext()
	}
	return c.OK("jev.doctor", fields).Write(c.Stdout)
}

var _ = context.Background
