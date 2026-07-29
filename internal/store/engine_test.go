package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureTree writes a small nested tree of YAML files and returns the root.
func fixtureTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("foo/bar/baz.yaml", "# baz service\nname: baz\nconfig:\n  port: 8080 # the port\n  enabled: false\n")
	write("services/web.yaml", "name: web\nimage: nginx\nreplicas: 3\n")
	write("services/api.yaml", "name: api\nimage: nginx\n")
	return root
}

func loadTree(t *testing.T, root string) []File {
	t.Helper()
	files, err := Scan(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func resolveOne(t *testing.T, files []File, field string) Match {
	t.Helper()
	m, ok := Deepest(Resolve(files, ParseField(field)))
	if !ok {
		t.Fatalf("no inside match for %q", field)
	}
	return m
}

func TestScanPrefixes(t *testing.T) {
	files := loadTree(t, fixtureTree(t))
	got := map[string]bool{}
	for _, f := range files {
		got[f.Prefix()] = true
	}
	for _, want := range []string{"foo.bar.baz", "services.web", "services.api"} {
		if !got[want] {
			t.Errorf("missing prefix %q; got %v", want, got)
		}
	}
}

func TestGetScalar(t *testing.T) {
	files := loadTree(t, fixtureTree(t))
	v, err := Get(resolveOne(t, files, "foo.bar.baz.config.port"))
	if err != nil {
		t.Fatal(err)
	}
	if v != "8080" {
		t.Fatalf("port = %q, want 8080", v)
	}
}

func TestExists(t *testing.T) {
	files := loadTree(t, fixtureTree(t))
	// present and non-null
	ok, err := Exists(resolveOne(t, files, "services.web.replicas"))
	if err != nil || !ok {
		t.Fatalf("replicas exists = %v, err %v", ok, err)
	}
	// absent field
	m, found := Deepest(Resolve(files, ParseField("services.api.replicas")))
	if !found {
		t.Fatal("expected a match candidate for api.replicas")
	}
	ok, err = Exists(m)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("api.replicas should not exist")
	}
}

func TestEquals(t *testing.T) {
	files := loadTree(t, fixtureTree(t))
	ok, err := Equals(resolveOne(t, files, "services.web.image"), "nginx", false)
	if err != nil || !ok {
		t.Fatalf("image==nginx = %v err %v", ok, err)
	}
	ok, _ = Equals(resolveOne(t, files, "services.web.replicas"), "3", false)
	if !ok {
		t.Fatal("replicas==3 should be true (numeric)")
	}
	ok, _ = Equals(resolveOne(t, files, "services.web.replicas"), "5", false)
	if ok {
		t.Fatal("replicas==5 should be false")
	}
}

func TestSetPreservesCommentsAndOrder(t *testing.T) {
	root := fixtureTree(t)
	files := loadTree(t, root)
	if err := Set(resolveOne(t, files, "foo.bar.baz.config.port"), "9090", false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "foo/bar/baz.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "port: 9090") {
		t.Fatalf("value not updated:\n%s", out)
	}
	if !strings.Contains(out, "# baz service") || !strings.Contains(out, "# the port") {
		t.Fatalf("comments not preserved:\n%s", out)
	}
	// key order: name before config
	if strings.Index(out, "name: baz") > strings.Index(out, "config:") {
		t.Fatalf("key order not preserved:\n%s", out)
	}
}

func TestSetRefusesWholeDoc(t *testing.T) {
	files := loadTree(t, fixtureTree(t))
	m := resolveOne(t, files, "services.web")
	if err := Set(m, "x", false); err == nil {
		t.Fatal("expected error setting whole document")
	}
}

func TestUnsetDeletesFieldPreservingSiblings(t *testing.T) {
	root := fixtureTree(t)
	files := loadTree(t, root)
	existed, err := Unset(resolveOne(t, files, "foo.bar.baz.config.port"))
	if err != nil || !existed {
		t.Fatalf("unset existed=%v err=%v", existed, err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "foo/bar/baz.yaml"))
	out := string(data)
	if strings.Contains(out, "port:") {
		t.Fatalf("port not deleted:\n%s", out)
	}
	if !strings.Contains(out, "enabled: false") || !strings.Contains(out, "# baz service") {
		t.Fatalf("siblings/comments not preserved:\n%s", out)
	}
}

func TestUnsetMissingFieldReportsFalse(t *testing.T) {
	root := fixtureTree(t)
	files := loadTree(t, root)
	before, _ := os.ReadFile(filepath.Join(root, "services/api.yaml"))
	m, _ := Deepest(Resolve(files, ParseField("services.api.replicas")))
	existed, err := Unset(m)
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Fatal("missing field should report not existed")
	}
	after, _ := os.ReadFile(filepath.Join(root, "services/api.yaml"))
	if string(before) != string(after) {
		t.Fatalf("file changed on no-op delete:\n%s", string(after))
	}
}

func TestUnsetPresentNullKey(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.yaml")
	if err := os.WriteFile(p, []byte("keep: 1\ngone:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := loadTree(t, root)
	existed, err := Unset(resolveOne(t, files, "a.gone"))
	if err != nil || !existed {
		t.Fatalf("null key: existed=%v err=%v", existed, err)
	}
	data, _ := os.ReadFile(p)
	if strings.Contains(string(data), "gone") {
		t.Fatalf("null key not deleted:\n%s", string(data))
	}
}

func TestUnsetRefusesWholeDoc(t *testing.T) {
	files := loadTree(t, fixtureTree(t))
	if _, err := Unset(resolveOne(t, files, "services.web")); err == nil {
		t.Fatal("expected error deleting whole document")
	}
}

func TestLiteralTyping(t *testing.T) {
	cases := map[string]string{
		"true":  "true",
		"42":    "42",
		"3.14":  "3.14",
		"null":  "null",
		"hello": `"hello"`,
		`a"b`:   `"a\"b"`,
	}
	for in, want := range cases {
		if got := literal(in, false); got != want {
			t.Errorf("literal(%q) = %q, want %q", in, got, want)
		}
	}
	if got := literal("42", true); got != `"42"` {
		t.Errorf("literal(42, asString) = %q, want \"42\"", got)
	}
}
