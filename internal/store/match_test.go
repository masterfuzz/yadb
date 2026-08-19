package store

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func fileWith(prefix string) File {
	return File{Path: strings.ReplaceAll(prefix, ".", "/") + ".yaml", Segments: strings.Split(prefix, ".")}
}

func TestMatchInside(t *testing.T) {
	tests := []struct {
		name     string
		pat      string
		pre      string
		leftover []string
		ok       bool
	}{
		{"exact literal path", "foo.bar.baz.config.port", "foo.bar.baz", []string{"config", "port"}, true},
		{"exact prefix whole doc", "foo.bar.baz", "foo.bar.baz", nil, true},
		{"pattern shorter than prefix is not inside", "foo.bar", "foo.bar.baz", nil, false},
		{"doublestar eats whole prefix", "**.name", "foo.bar", []string{"name"}, true},
		{"doublestar mid pattern", "foo.**.port", "foo.bar.baz", []string{"port"}, true},
		{"single star one segment", "services.*.replicas", "services.web", []string{"replicas"}, true},
		{"literal mismatch", "foo.qux.port", "foo.bar", nil, false},
		{"doublestar trailing whole doc", "services.**", "services.web", nil, true},
		{"doublestar then literal matching file's own segment, zero nesting", "a.**.two", "a.two", nil, true},
		{"doublestar then literal matching file's own segment, one level nesting", "a.**.two", "a.b.two", nil, true},
		{"doublestar then literal matching file's own segment, two levels nesting", "a.**.two", "a.b.c.two", nil, true},
		{"doublestar then literal segment plus infile suffix, nested", "a.**.two.name", "a.b.c.two", []string{"name"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leftover, ok := matchInside(strings.Split(tt.pat, "."), strings.Split(tt.pre, "."))
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && len(leftover)+len(tt.leftover) > 0 && !reflect.DeepEqual(leftover, tt.leftover) {
				t.Fatalf("leftover = %#v, want %#v", leftover, tt.leftover)
			}
		})
	}
}

func TestMatchAncestor(t *testing.T) {
	rem, ok := matchAncestor([]string{"foo", "bar"}, []string{"foo", "bar", "baz"})
	if !ok || !reflect.DeepEqual(rem, []string{"baz"}) {
		t.Fatalf("ancestor rem=%#v ok=%v", rem, ok)
	}
	if _, ok := matchAncestor([]string{"foo", "bar"}, []string{"foo", "bar"}); ok {
		t.Fatal("exact match must not be reported as ancestor")
	}
	if _, ok := matchAncestor([]string{"foo", "bar", "baz"}, []string{"foo", "bar"}); ok {
		t.Fatal("longer pattern must not be an ancestor")
	}
}

func TestResolveDeepestWins(t *testing.T) {
	files := []File{fileWith("foo.bar"), fileWith("foo.bar.baz")}
	matches := Resolve(files, ParseField("foo.bar.baz.config.port"))
	// Both files can contain the field as an in-file path; Deepest picks the
	// nested file.
	m, ok := Deepest(matches)
	if !ok {
		t.Fatal("expected a deepest match")
	}
	if m.File.Prefix() != "foo.bar.baz" {
		t.Fatalf("deepest = %q, want foo.bar.baz", m.File.Prefix())
	}
	if m.Expr() != ".config.port" {
		t.Fatalf("expr = %q, want .config.port", m.Expr())
	}
}

func TestResolveWildcardMatchesMany(t *testing.T) {
	files := []File{fileWith("services.web"), fileWith("services.api"), fileWith("other.thing")}
	matches := InsideMatches(Resolve(files, ParseField("services.*.replicas")))
	var got []string
	for _, m := range matches {
		got = append(got, m.File.Prefix()+"|"+m.Expr())
	}
	sort.Strings(got)
	want := []string{"services.api|.replicas", "services.web|.replicas"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestResolveAncestorMatch(t *testing.T) {
	files := []File{fileWith("foo.bar.baz"), fileWith("foo.other")}
	matches := Resolve(files, ParseField("foo.bar"))
	var ancestors []string
	for _, m := range matches {
		if m.Ancestor {
			ancestors = append(ancestors, m.File.Prefix())
		}
	}
	if !reflect.DeepEqual(ancestors, []string{"foo.bar.baz"}) {
		t.Fatalf("ancestor files = %#v, want [foo.bar.baz]", ancestors)
	}
}

func TestResolveDoublestarAcrossMultipleNestingLevels(t *testing.T) {
	// "two" is both a literal segment in the field and the final path segment
	// of files nested at varying depths under "a". The field should resolve
	// to each file's in-file "name" path regardless of how many segments "**"
	// has to span to reach the literal "two".
	files := []File{
		fileWith("a.two"),
		fileWith("a.b.two"),
		fileWith("a.b.c.two"),
	}
	matches := InsideMatches(Resolve(files, ParseField("a.**.two.name")))
	var got []string
	for _, m := range matches {
		got = append(got, m.File.Prefix()+"|"+m.Expr())
	}
	sort.Strings(got)
	want := []string{"a.b.c.two|.name", "a.b.two|.name", "a.two|.name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestExprQuotesSpecialKeys(t *testing.T) {
	m := Match{InFile: []string{"metadata", "my-label"}}
	if got := m.Expr(); got != `.metadata.["my-label"]` {
		t.Fatalf("expr = %q", got)
	}
}
