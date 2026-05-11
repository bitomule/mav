# Contributing

Thanks for helping improve MAV.

MAV is a deterministic CLI for agent-driven iOS validation. Changes should keep
that contract intact: commands execute concrete work, write artifacts, and
return compact output that an agent can parse.

## Development

```bash
make test
make build
make check
```

`make check` runs formatting, tests, and a local build.

## Design Principles

- Keep commands deterministic. MAV should not make exploratory decisions for the
  caller.
- Keep default output compact: one line with `ok cmd=...` or
  `fail code=...`.
- Put detailed artifacts in the run directory instead of stdout.
- Prefer accessibility tree data over screenshots for agent decisions.
- Prefer accessibility ids for UI targeting, coordinates as a visual fallback,
  and text as the last fallback.
- Keep simulator/device process ownership explicit. Long-running processes must
  be tracked in the run directory and stopped by `mav stop` or `mav run`.

## Pull Requests

Before opening a PR:

1. Run `make check`.
2. Update `README.md` and `skills/mav/SKILL.md` when command behavior changes.
3. Add or update tests for new output contracts and flow steps.
4. Include enough context in the PR description to explain the validation path.

## Security

Do not report security issues in public issues. Contact the maintainer directly
until a private disclosure process is published.
