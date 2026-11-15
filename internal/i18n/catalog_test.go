package i18n

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const importPath = "github.com/Georgy-Garnov/agendling/internal/i18n"

// msgKind says where a key must be found in a catalog.
type msgKind int

const (
	kindMessage msgKind = iota // Messages
	kindPlural                 // Plurals, keyed by the English singular
)

type msgUse struct {
	kind  msgKind
	other string // the English plural, for kindPlural
	pos   string // first use, "file.go:12"
}

// keyArgs maps each i18n function to the indexes of its message arguments.
var keyArgs = map[string][]int{"T": {0}, "Tf": {0}, "Mark": {0}, "Date": {1}, "TN": {1, 2}}

// mustBeLiteral lists the functions whose message must be written in place: T alone may
// translate a value marked elsewhere (a table field).
var mustBeLiteral = map[string]bool{"Tf": true, "Mark": true, "Date": true, "TN": true}

// scanMessages collects the messages passed to this package in the Go files under root,
// skipping tests, hidden directories and testdata. Problems are calls the test cannot read.
func scanMessages(root string) (msgs map[string]msgUse, problems []string, err error) {
	msgs = map[string]msgUse{}
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		local := importName(f)
		if local == "" {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != local {
				return true
			}
			idx, ok := keyArgs[sel.Sel.Name]
			if !ok || len(call.Args) <= idx[len(idx)-1] {
				return true
			}
			rel, _ := filepath.Rel(root, fset.Position(call.Pos()).Filename)
			pos := fmt.Sprintf("%s:%d", filepath.ToSlash(rel), fset.Position(call.Pos()).Line)
			var texts []string
			for _, i := range idx {
				s, ok := stringLiteral(call.Args[i])
				if !ok {
					if mustBeLiteral[sel.Sel.Name] {
						problems = append(problems, fmt.Sprintf("%s: %s.%s needs a string literal", pos, local, sel.Sel.Name))
					}
					return true
				}
				texts = append(texts, s)
			}
			use := msgUse{kind: kindMessage, pos: pos}
			if sel.Sel.Name == "TN" {
				use = msgUse{kind: kindPlural, other: texts[1], pos: pos}
			}
			if prev, seen := msgs[texts[0]]; seen {
				if prev.kind != use.kind || prev.other != use.other {
					problems = append(problems, fmt.Sprintf("%s: %q is used differently at %s", pos, texts[0], prev.pos))
				}
				return true
			}
			msgs[texts[0]] = use
			return true
		})
		return nil
	})
	return msgs, problems, err
}

// importName returns the name this package is imported under in f, or "".
func importName(f *ast.File) string {
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == importPath {
			if imp.Name != nil {
				return imp.Name.Name
			}
			return "i18n"
		}
	}
	return ""
}

// stringLiteral reads a string constant, including one split with + over several lines.
func stringLiteral(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		a, ok1 := stringLiteral(e.X)
		b, ok2 := stringLiteral(e.Y)
		return a + b, ok1 && ok2
	case *ast.ParenExpr:
		return stringLiteral(e.X)
	}
	return "", false
}

var verbRe = regexp.MustCompile(`%[-+# 0]*(\[\d+\])?\d*(\.\d+)?[a-zA-Z%]`)

// verbs returns the fmt verbs of s, sorted, so translations may reorder them with %[n]v.
func verbs(s string) []string {
	var res []string
	for _, v := range verbRe.FindAllString(s, -1) {
		if v != "%%" {
			res = append(res, v[len(v)-1:])
		}
	}
	sort.Strings(res)
	return res
}

// checkCatalog lists what c lacks or has in excess for the messages used in the code.
func checkCatalog(c *Catalog, msgs map[string]msgUse) []string {
	var problems []string
	for key, use := range msgs {
		switch use.kind {
		case kindMessage:
			tr := c.Messages[key]
			if tr == "" {
				problems = append(problems, fmt.Sprintf("missing translation of %q (%s)", key, use.pos))
			} else if !slices.Equal(verbs(tr), verbs(key)) {
				problems = append(problems, fmt.Sprintf("translation of %q has verbs %v, want %v", key, verbs(tr), verbs(key)))
			}
		case kindPlural:
			forms := c.Plurals[key]
			if len(forms) == 0 {
				problems = append(problems, fmt.Sprintf("missing plural forms of %q (%s)", key, use.pos))
			}
			for _, n := range []int{0, 1, 2, 3, 5, 11, 12, 21, 22, 25, 100, 101, 111, 1000, 1001} {
				if i := c.PluralForm(n); i < 0 || i >= len(forms) {
					if len(forms) > 0 {
						problems = append(problems, fmt.Sprintf("plural %q has no form %d (for n=%d)", key, i, n))
					}
					break
				}
			}
			for _, f := range forms {
				if f == "" || !slices.Equal(verbs(f), verbs(use.other)) {
					problems = append(problems, fmt.Sprintf("plural form %q of %q does not match %q", f, key, use.other))
				}
			}
		}
	}
	for key := range c.Messages {
		if use, ok := msgs[key]; !ok || use.kind != kindMessage {
			problems = append(problems, fmt.Sprintf("unused translation of %q", key))
		}
	}
	for key := range c.Plurals {
		if use, ok := msgs[key]; !ok || use.kind != kindPlural {
			problems = append(problems, fmt.Sprintf("unused plural forms of %q", key))
		}
	}
	names := slices.Concat(c.Days[:], c.ShortDays[:], c.Months[:], c.ShortMonths[:])
	if slices.Contains(names, "") {
		problems = append(problems, "day or month names are missing")
	}
	if c.Name == "" {
		problems = append(problems, "the language has no name")
	}
	sort.Strings(problems)
	return problems
}

func moduleRoot(t *testing.T) string {
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
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// TestCatalogsAreComplete checks every registered language against the messages the code
// uses: each needs a translation with the same fmt verbs, and none may be left over.
func TestCatalogsAreComplete(t *testing.T) {
	msgs, problems, err := scanMessages(moduleRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
	// Guards against the scan silently finding nothing (a renamed package, a moved file).
	for _, key := range []string{"(No title)", "Monday, 2 January 2006", "%d calendar", "Calendars"} {
		if _, ok := msgs[key]; !ok {
			t.Errorf("scan did not find %q; found %d messages", key, len(msgs))
		}
	}
	regMu.RLock()
	defer regMu.RUnlock()
	for tag, c := range registry {
		if c == English {
			continue
		}
		for _, p := range checkCatalog(c, msgs) {
			t.Errorf("%s: %s", tag, p)
		}
	}
}

func TestScanMessages(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a/a.go", `package a

import tr "`+importPath+`"

var names = []string{tr.Mark("Day"), tr.Mark("Week")}

func f(n int, label, layout string) {
	_ = tr.T("Hello")
	_ = tr.T(label) // a marked value: allowed
	_ = tr.Tf("in %s", "x")
	_ = tr.TN(n, "%d item", "%d items")
	_ = tr.Date(time.Time{}, "Monday, 2 January" +
		" 2006")
	_ = tr.Date(time.Time{}, layout)
	_ = tr.Mark(label)
	_ = tr.T("Hello")
}
`)
	write("b/b_test.go", `package b

import "`+importPath+`"

var _ = i18n.T("only in a test")
`)
	write("c/c.go", `package c

func T(s string) string { return s }

var _ = T("not ours")
`)
	write(".hidden/h.go", `package h

import "`+importPath+`"

var _ = i18n.T("hidden")
`)
	write("d/d.go", `package d

import "`+importPath+`"

var _ = i18n.TN(2, "%d item", "%d things")
`)

	msgs, problems, err := scanMessages(root)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range msgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"%d item", "Day", "Hello", "Monday, 2 January 2006", "Week", "in %s"}
	if !slices.Equal(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
	if msgs["%d item"].kind != kindPlural || msgs["%d item"].other != "%d items" {
		t.Errorf("plural = %+v", msgs["%d item"])
	}
	if len(problems) != 3 {
		t.Fatalf("problems = %q, want the non-literal Date and Mark and the conflicting plural", problems)
	}
	for i, frag := range []string{"tr.Date needs a string literal", "tr.Mark needs a string literal", `"%d item" is used differently`} {
		if !strings.Contains(strings.Join(problems, "\n"), frag) {
			t.Errorf("problem %d: no %q in %q", i, frag, problems)
		}
	}
}

func TestCheckCatalog(t *testing.T) {
	msgs := map[string]msgUse{
		"Settings":      {kind: kindMessage, pos: "x.go:1"},
		"starts in %s":  {kind: kindMessage, pos: "x.go:2"},
		"%d of %d":      {kind: kindMessage, pos: "x.go:3"},
		"%d calendar":   {kind: kindPlural, other: "%d calendars", pos: "x.go:4"},
		"Missing":       {kind: kindMessage, pos: "x.go:5"},
		"%d event left": {kind: kindPlural, other: "%d events left", pos: "x.go:6"},
	}
	c := testCatalog()
	c.Messages = map[string]string{
		"Settings":     "Réglages",
		"starts in %s": "commence dans %d", // wrong verb
		"%d of %d":     "%[2]d sur %[1]d",  // reordered: fine
		"Unused":       "Inutilisé",
	}
	c.Plurals = map[string][]string{
		"%d calendar":   {"%d calendrier", "%d calendriers", "%d calendriers"},
		"%d event left": {"%d événement restant", "%d événements restants"}, // the rule needs 3 forms
	}
	got := checkCatalog(c, msgs)
	want := []string{
		`day or month names are missing`,
		`missing translation of "Missing" (x.go:5)`,
		`plural "%d event left" has no form 2 (for n=0)`,
		`translation of "starts in %s" has verbs [d], want [s]`,
		`unused translation of "Unused"`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("checkCatalog:\n got  %q\n want %q", got, want)
	}
}
