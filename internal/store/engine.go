package store

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mikefarah/yq/v4/pkg/yqlib"
)

func init() {
	// yqlib logs warnings to stderr by default (e.g. for missing paths during
	// traversal); quiet it so yadb output stays clean.
	yqlib.GetLogger().SetLevel(slog.LevelError)
}

// eval runs a yq expression against a YAML document string, preserving comments
// and key order, and returns the rendered result.
func eval(expr, input string) (string, error) {
	prefs := yqlib.NewDefaultYamlPreferences()
	prefs.PrintDocSeparators = false
	enc := yqlib.NewYamlEncoder(prefs)
	dec := yqlib.NewYamlDecoder(prefs)
	out, err := yqlib.NewStringEvaluator().EvaluateAll(expr, input, enc, dec)
	if err != nil {
		return "", err
	}
	return out, nil
}

// Get evaluates the match's in-file path against its file and returns the
// value, with any single trailing newline trimmed for display.
func Get(m Match) (string, error) {
	input, err := os.ReadFile(m.File.Path)
	if err != nil {
		return "", err
	}
	out, err := eval(m.Expr(), string(input))
	if err != nil {
		return "", fmt.Errorf("%s: %w", m.File.Path, err)
	}
	return strings.TrimSuffix(out, "\n"), nil
}

// Exists reports whether the match's in-file path is present and non-null.
func Exists(m Match) (bool, error) {
	input, err := os.ReadFile(m.File.Path)
	if err != nil {
		return false, err
	}
	out, err := eval(m.Expr()+" | select(. != null)", string(input))
	if err != nil {
		return false, fmt.Errorf("%s: %w", m.File.Path, err)
	}
	return out != "", nil
}

// Equals reports whether the match's in-file path equals the given value. The
// value is interpreted with the same type inference used by Set unless
// asString forces a string comparison.
func Equals(m Match, value string, asString bool) (bool, error) {
	input, err := os.ReadFile(m.File.Path)
	if err != nil {
		return false, err
	}
	expr := fmt.Sprintf("[%s == %s] | any", m.Expr(), literal(value, asString))
	out, err := eval(expr, string(input))
	if err != nil {
		return false, fmt.Errorf("%s: %w", m.File.Path, err)
	}
	return strings.TrimSpace(out) == "true", nil
}

// Set assigns the given value at the match's in-file path and writes the file
// back in place, preserving comments and key order.
func Set(m Match, value string, asString bool) error {
	if len(m.InFile) == 0 {
		return fmt.Errorf("%s: refusing to set the entire document (%s); specify a field path",
			m.File.Path, m.File.Prefix())
	}
	input, err := os.ReadFile(m.File.Path)
	if err != nil {
		return err
	}
	expr := fmt.Sprintf("%s = %s", m.Expr(), literal(value, asString))
	out, err := eval(expr, string(input))
	if err != nil {
		return fmt.Errorf("%s: %w", m.File.Path, err)
	}
	return writeFileAtomic(m.File.Path, []byte(out))
}

// literal converts a raw command-line value into a yq expression literal.
// Unless asString is set, values that look like booleans, null, integers or
// floats are emitted unquoted so they land as the corresponding YAML types.
func literal(value string, asString bool) string {
	if asString {
		return quote(value)
	}
	switch value {
	case "true", "false", "null", "~":
		return value
	}
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		return value
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return value
	}
	return quote(value)
}

func quote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

// writeFileAtomic writes data to path via a temporary file and a rename,
// preserving the original file's permissions when it exists.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".yadb-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
