package repocheck

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// root is the repository root: the nearest directory above the test's
// working directory that holds go.mod.
func root(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// tracked lists the files git tracks plus the ones staged for addition, as
// slash paths relative to the root. A tree that is not a git checkout (a
// module download) has nothing to check.
func tracked(t *testing.T, dir string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "ls-files", "--cached").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); errors.Is(err, os.ErrNotExist) {
			continue // deleted in the working tree, not yet committed
		}
		files = append(files, f)
	}
	return files
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(dir, rel string) bool {
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil
}

// ---- pure helpers ----

var linkRe = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)

// relativeLinks returns the relative targets of markdown links, without
// fragments; absolute URLs and mailto links are skipped.
func relativeLinks(markdown string) []string {
	var links []string
	for _, m := range linkRe.FindAllStringSubmatch(markdown, -1) {
		target, _, _ := strings.Cut(strings.TrimSpace(m[1]), "#")
		if target == "" || strings.HasPrefix(target, "http:") || strings.HasPrefix(target, "https:") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		links = append(links, target)
	}
	return links
}

// patternRegexp compiles a routing pattern: `**` crosses directories, `*`
// stays inside one path segment.
func patternRegexp(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch {
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i++
		case pattern[i] == '*':
			b.WriteString("[^/]*")
		default:
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

type route struct {
	patterns []string
	docs     []string
}

var backtickRe = regexp.MustCompile("`([^`]+)`")

func backticked(cell string) []string {
	var out []string
	for _, m := range backtickRe.FindAllStringSubmatch(cell, -1) {
		if m[1] != "—" && m[1] != "" {
			out = append(out, m[1])
		}
	}
	return out
}

// routingTable parses every markdown table whose header names a path column
// and a doc column.
func routingTable(markdown string) []route {
	var routes []route
	var table []string
	flush := func() {
		if len(table) >= 3 {
			header := cells(table[0])
			if len(header) >= 2 && strings.Contains(strings.ToLower(header[0]), "path") && strings.Contains(strings.ToLower(header[1]), "doc") {
				for _, row := range table[2:] {
					c := cells(row)
					if len(c) < 2 {
						continue
					}
					if p := backticked(c[0]); len(p) > 0 {
						routes = append(routes, route{patterns: p, docs: backticked(c[1])})
					}
				}
			}
		}
		table = nil
	}
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			table = append(table, line)
		} else {
			flush()
		}
	}
	flush()
	return routes
}

func cells(row string) []string {
	parts := strings.Split(row, "|")
	if len(parts) < 3 {
		return nil
	}
	parts = parts[1 : len(parts)-1]
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func routed(routes []route, file string) bool {
	for _, r := range routes {
		for _, p := range r.patterns {
			if patternRegexp(p).MatchString(file) {
				return true
			}
		}
	}
	return false
}

var (
	frontmatterLine = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_-]*):\s*(.*)$`)
	kebab           = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// skillFailures checks the open SKILL.md frontmatter contract: a name that
// matches the directory and is kebab-case (≤ 64 chars), and a description of
// at most 1024 chars.
func skillFailures(dirName, text string) []string {
	if !strings.HasPrefix(text, "---\n") {
		return []string{"frontmatter: missing opening fence"}
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return []string{"frontmatter: missing closing fence"}
	}
	data := map[string]string{}
	for _, line := range strings.Split(text[4:4+end], "\n") {
		if m := frontmatterLine.FindStringSubmatch(line); m != nil {
			data[m[1]] = m[2]
		}
	}
	var failures []string
	switch name := data["name"]; {
	case name == "":
		failures = append(failures, "frontmatter: missing name")
	case name != dirName:
		failures = append(failures, "frontmatter: name "+name+" does not match directory "+dirName)
	case !kebab.MatchString(name) || len(name) > 64:
		failures = append(failures, "frontmatter: name "+name+" is not kebab-case within 64 chars")
	}
	switch desc := strings.TrimSpace(data["description"]); {
	case desc == "":
		failures = append(failures, "frontmatter: missing description")
	case len([]rune(desc)) > 1024:
		failures = append(failures, "frontmatter: description longer than 1024 chars")
	}
	return failures
}

var requireLine = regexp.MustCompile(`^\s*(?:require\s+)?([a-z0-9.-]+\.[a-z]+/\S+)\s+v\S+(\s*//\s*indirect)?\s*$`)

// directDeps lists go.mod requirements that are not marked indirect.
func directDeps(gomod string) []string {
	var deps []string
	for _, line := range strings.Split(gomod, "\n") {
		if m := requireLine.FindStringSubmatch(line); m != nil && m[2] == "" {
			deps = append(deps, m[1])
		}
	}
	return deps
}

// ---- repo checks ----

func TestDirectDependenciesAreJustified(t *testing.T) {
	dir := root(t)
	contributing := read(t, dir, "CONTRIBUTING.md")
	for _, dep := range directDeps(read(t, dir, "go.mod")) {
		if !strings.Contains(contributing, "`"+dep+"`") {
			t.Errorf("go.mod requires %s directly, but CONTRIBUTING.md's dependency table does not justify it", dep)
		}
	}
}

func TestSkillContracts(t *testing.T) {
	dir := root(t)
	entries, err := os.ReadDir(filepath.Join(dir, "skills"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		rel := path.Join("skills", e.Name(), "SKILL.md")
		if !exists(dir, rel) {
			t.Errorf("skills/%s: no SKILL.md", e.Name())
			continue
		}
		text := read(t, dir, rel)
		for _, f := range skillFailures(e.Name(), text) {
			t.Errorf("%s: %s", rel, f)
		}
		for _, link := range relativeLinks(text) {
			if !exists(dir, path.Join("skills", e.Name(), link)) {
				t.Errorf("%s: broken link %s", rel, link)
			}
		}
	}
}

func TestDocLinksResolve(t *testing.T) {
	dir := root(t)
	for _, f := range tracked(t, dir) {
		if !strings.HasSuffix(f, ".md") {
			continue
		}
		for _, link := range relativeLinks(read(t, dir, f)) {
			if !exists(dir, path.Join(path.Dir(f), link)) {
				t.Errorf("%s: broken link %s", f, link)
			}
		}
	}
}

func TestDocRoutingCoversEveryPath(t *testing.T) {
	dir := root(t)
	routes := routingTable(read(t, dir, "docs/index.md"))
	if len(routes) == 0 {
		t.Fatal("docs/index.md: no routing table found")
	}
	for _, f := range tracked(t, dir) {
		if !routed(routes, f) {
			t.Errorf("no doc route in docs/index.md for tracked path %s", f)
		}
	}
	for _, r := range routes {
		for _, doc := range r.docs {
			if !exists(dir, doc) {
				t.Errorf("docs/index.md: owner doc %s does not exist", doc)
			}
		}
	}
}

var binaryExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".pdf": true, ".zip": true, ".ico": true}

func TestNoRedlineTerms(t *testing.T) {
	dir := root(t)
	raw, err := os.ReadFile(filepath.Join(dir, ".redlines.local.json"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skip(".redlines.local.json not found")
	}
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Terms []string `json:"terms"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf(".redlines.local.json: %v", err)
	}
	for _, f := range tracked(t, dir) {
		if binaryExt[strings.ToLower(path.Ext(f))] {
			continue
		}
		for i, line := range strings.Split(strings.ToLower(read(t, dir, f)), "\n") {
			for _, term := range cfg.Terms {
				if term = strings.ToLower(strings.TrimSpace(term)); term != "" && strings.Contains(line, term) {
					t.Errorf("%s:%d: redline term %q", f, i+1, term)
				}
			}
		}
	}
}

// ---- helper tests ----

func TestRelativeLinks(t *testing.T) {
	got := relativeLinks("[a](docs/x.md#top) [b](https://e.com) [c](mailto:x@y) [d](../y.md)")
	if strings.Join(got, ",") != "docs/x.md,../y.md" {
		t.Fatalf("relativeLinks = %v", got)
	}
}

func TestPatternRegexp(t *testing.T) {
	for _, c := range []struct {
		pattern, file string
		want          bool
	}{
		{"internal/**", "internal/cli/cli.go", true},
		{"docs/*.md", "docs/index.md", true},
		{"docs/*.md", "docs/adr/0001.md", false},
		{"go.mod", "go.mod", true},
		{"go.mod", "go.modx", false},
	} {
		if got := patternRegexp(c.pattern).MatchString(c.file); got != c.want {
			t.Errorf("%s ~ %s = %v, want %v", c.pattern, c.file, got, c.want)
		}
	}
}

func TestRoutingTable(t *testing.T) {
	md := "| Changed path | Docs to check |\n| --- | --- |\n| `a/**`, `b.go` | `README.md` |\n| `LICENSE` | — |\n\ntext"
	routes := routingTable(md)
	if len(routes) != 2 || len(routes[0].patterns) != 2 || routes[0].docs[0] != "README.md" || len(routes[1].docs) != 0 {
		t.Fatalf("routingTable = %+v", routes)
	}
}

func TestSkillFailures(t *testing.T) {
	ok := "---\nname: muster\ndescription: Launch agents.\n---\nbody"
	if f := skillFailures("muster", ok); len(f) != 0 {
		t.Fatalf("valid skill failed: %v", f)
	}
	if f := skillFailures("other", ok); len(f) != 1 {
		t.Fatalf("name/dir mismatch not caught: %v", f)
	}
	if f := skillFailures("muster", "no frontmatter"); len(f) != 1 {
		t.Fatalf("missing fence not caught: %v", f)
	}
}

func TestDirectDeps(t *testing.T) {
	gomod := "module m\n\ngo 1.25.0\n\nrequire (\n\tgolang.org/x/term v0.45.0\n\tgolang.org/x/sys v0.47.0 // indirect\n)\n\nrequire filippo.io/age v1.2.1\n"
	if got := strings.Join(directDeps(gomod), ","); got != "golang.org/x/term,filippo.io/age" {
		t.Fatalf("directDeps = %s", got)
	}
}
