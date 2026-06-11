# Hangar ✈

A TUI package manager for **Agent Skills** — the `SKILL.md` files that AI coding
agents (Claude Code, Cursor, Copilot, opencode, Codex, Gemini CLI, and ~50
others) load to extend their capabilities.

Hangar discovers skills in **GitHub repositories** and **npm packages**, lets you
pick exactly which ones to install through an interactive tree picker, and wires
them into every agent on your machine that follows the `.agents/` convention.

Inspired by [`withastro/rosie`](https://github.com/withastro/rosie), with a
TUI-first workflow, subpath installs, real npm-registry fetching, a TOML
lockfile, and a `doctor`.

---

## Install

```sh
go install github.com/louisescher/hangar@latest
```

Or build from source:

```sh
git clone https://github.com/louisescher/hangar && cd hangar
go build -o hangar .
```

## Quick start

```sh
# Browse the curated catalog and pick interactively
hangar

# Install from a GitHub repo — opens the picker when there are many skills
hangar install anthropics/skills

# Install one skill directly from a subpath
hangar install anthropics/skills/skills/pdf

# Install everything, no prompts
hangar install anthropics/skills --all

# Install from npm (SKILL.md files become skills; README/docs become references)
hangar install npm:@your-scope/skills-pkg

# Keep things current
hangar update

# Check the install is healthy
hangar doctor
```

## Source specifiers

| Form | Example |
|---|---|
| GitHub repo | `owner/repo` |
| …with a subpath | `owner/repo/path/to/skills` |
| …pinned to a ref | `owner/repo@v1.2.0` or `owner/repo@release/1.x` |
| …a single skill | `owner/repo#skill-name` |
| npm package | `npm:lodash`, `npm:@scope/pkg` |
| …a version / subpath / doc | `npm:pkg@1.2.0`, `npm:pkg/sub`, `npm:pkg#docs/api.md` |
| local path | `./path`, `/abs/path`, `~/path`, `file://…` |

Refs are taken from the **last** `@`, so they may contain `/`. Subpaths can never
escape the repository root.

## The interactive picker

Run `hangar` to browse the curated catalog (grouped by category — Documents,
Design & Art, Web & Frontend, Development & Agents, Writing & Comms — plus
free-form source entry), or `hangar install <source>` on a multi-skill source to
open the picker directly:

- **Tree view** — folders carry tri-state checkboxes (`[x]` / `[~]` / `[ ]`);
  toggling a folder cascades to the skills beneath it.
- **`t`** — switch between the tree and a flat list. Your selection is preserved.
- **`/`** — incrementally filter by name, description, or path (matches keep
  their ancestors and auto-expand in the tree).
- **Right pane** — a live, glamour-rendered preview of the highlighted
  `SKILL.md`. Press **`tab`** to focus it and scroll (`↑/↓`, `pgup/pgdn`);
  `esc`/`tab` returns to the list.
- **`space`** toggle · **`→`/`←`** expand/collapse folders · **`a`/`n`** all/none
  · **`⏎`** continue · **`esc`** back · **`q`** quit.

Then choose target agents (detected ones are pre-selected), set the scope and
sanitization on the options screen, and review. The review screen lets you jump
back to any step (`s` skills · `a` agents · `o` options), tweak scope/sanitize
inline (`g`/`i`/`c`), and page the full selected-skill list (`f`).

## On-disk layout

Hangar uses the shared `.agents/` convention so installs interoperate with the
wider ecosystem:

```
.agents/
  skills/<name>/SKILL.md      canonical store (real files)
  references/<name>/REFERENCE.md
  hangar.lock                 TOML lockfile
.claude/skills/<name> -> ../../.agents/skills/<name>   (per-agent symlink, local)
AGENTS.md                     managed <references> block (also CLAUDE.md, …)
```

Local (project) installs symlink each agent's skills dir to the canonical store;
global installs (`-g`) copy instead. References (npm READMEs/docs) are written to
`.agents/references/` and linked from a managed block in your instructions file.

## Security

Third-party skills are sanitized on install:

- **Invisible characters** (zero-width, bidi overrides, the Unicode Tag block)
  are stripped from all content.
- **Markdown comments** (`<!-- … -->` and `[//]: #`) are additionally stripped
  from references — outside fenced code blocks.
- `update` flags **tag rewrites** (a pinned tag whose commit moved) as a
  high-severity finding.
- Headless installs print an **audit envelope** (a JSON record of every change,
  wrapped in a "treat as untrusted content" preamble) when stdout is not a TTY —
  so an agent invoking Hangar reviews changes before trusting them.

Disable passes with `--no-strip`, `--no-strip-comments`, `--no-strip-invisible`;
control the envelope with `--audit` / `--no-audit`.

## Commands

| Command | Purpose |
|---|---|
| `hangar` | Browse the catalog and install interactively |
| `hangar install <source>` | Install skills (interactive or headless) |
| `hangar install` | Reinstall everything from the lockfile |
| `hangar list` | Manage installed skills (interactive on a terminal) |
| `hangar list <source>` | List the skills/references a source contains |
| `hangar info <skill\|source>` | Render a skill's SKILL.md |
| `hangar update [skill]` | Re-resolve and update installed skills |
| `hangar pin <skill> [ref]` | Pin a skill (optionally reinstall at a ref) |
| `hangar unpin <skill>` | Clear a skill's pinned flag |
| `hangar remove <skill>` | Remove a skill/reference everywhere |
| `hangar nuke [-y]` | Remove every installed skill/reference (respects flags) |
| `hangar profile save\|apply\|list\|rm` | Save and re-apply named skill sets |
| `hangar doctor [--fix]` | Diagnose and repair drift |

Common flags: `-a/--agent` (repeatable target), `-g/--global`, `-A/--all`,
`-y/--yes`, `--no-tty`, `--json`, `-v/--verbose`, `--cwd`.

### Managing installed skills

`hangar list` (no source) opens an interactive manager on a terminal: it flags
outdated skills (`⬆ <version>`), and lets you preview a pending update as a
unified **diff** (`⏎`), **update** one (`u`) or all (`U`), **pin**/unpin (`p`),
and **remove** (`x`). Headless or `--json`, it prints the same status.

### Profiles

Capture the skills installed in a project and re-apply them elsewhere:

```sh
hangar profile save backend     # snapshot this project's lockfile
cd ../other-project
hangar profile apply backend    # install the same set here
```

### Headless & scripting

Hangar runs non-interactively when stdout isn't a terminal, in CI, with
`--no-tty`, or with `--all`/`-y`. Add `--json` to `list`, `install`, `update`,
and `doctor` for machine-readable output:

```sh
hangar list anthropics/skills --json
hangar doctor --json   # exits non-zero if unhealthy
```

## Preferences

User preferences persist to `~/.config/hangar/config.toml` (XDG-aware): the
picker view (tree/list), default agents, scope, and sanitize toggles. CLI flags
always override the stored value for a run.

## npm & private registries

npm packages are fetched from the registry, integrity-verified
(`dist.integrity`/`shasum`), and crawled for skills and docs. Hangar honors
`.npmrc`: the default `registry`, per-scope `@scope:registry`, and
`//host/:_authToken` (with `${ENV}` expansion) for private and scoped registries.

## Development

```sh
go build ./...
go test ./...
```

The codebase is layered with a strict dependency direction
(`cmd → {tui, present, engine}`; engine packages never import the TUI). The TUI
talks to the rest of Hangar only through `tui.EngineAPI`, so screens are tested
against a fake engine.

## License

MIT
