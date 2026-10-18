// Package repocheck holds the repo guardrails as tests, so `go test ./...` is
// the single gate: every direct dependency is justified in CONTRIBUTING.md,
// skills follow the SKILL.md contract, relative doc links resolve, the doc
// routing table in docs/index.md covers every tracked path, and the local
// redline terms (.redlines.local.json, gitignored) appear nowhere.
package repocheck
