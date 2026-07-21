// Package store maps a tree of YAML files onto a single unified dotted
// namespace. A file at foo/bar/baz.yaml contributes its contents under the
// prefix "foo.bar.baz", so a field addressed as "foo.bar.baz.config.port"
// resolves to the ".config.port" path inside that file.
package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// File is a single YAML file discovered under the root, together with the
// dotted namespace prefix derived from its path.
type File struct {
	// Path is the path to the file (relative to the process working dir, as
	// given by the walk), suitable for reading and display.
	Path string
	// Segments are the prefix segments derived from the file's location,
	// e.g. foo/bar/baz.yaml -> ["foo", "bar", "baz"].
	Segments []string
}

// Prefix is the dotted namespace prefix for the file, e.g. "foo.bar.baz".
func (f File) Prefix() string { return strings.Join(f.Segments, ".") }

// DefaultExts are the file extensions treated as YAML when none are supplied.
var DefaultExts = []string{".yaml", ".yml"}

// Scan walks root recursively and returns every YAML file (by extension),
// sorted by path for deterministic output. Extensions must include the
// leading dot; matching is case-insensitive.
func Scan(root string, exts []string) ([]File, error) {
	if len(exts) == 0 {
		exts = DefaultExts
	}
	extSet := make(map[string]struct{}, len(exts))
	for _, e := range exts {
		extSet[strings.ToLower(e)] = struct{}{}
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root %q is not a directory", root)
	}

	var files []File
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := extSet[strings.ToLower(filepath.Ext(path))]; !ok {
			return nil
		}
		segs, err := segmentsFor(root, path)
		if err != nil {
			return err
		}
		files = append(files, File{Path: path, Segments: segs})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// segmentsFor derives the dotted namespace segments for a file relative to
// root. The extension is dropped and both path separators and dots within
// path components act as segment separators, so foo/bar/baz.prod.yaml yields
// ["foo", "bar", "baz", "prod"].
func segmentsFor(root, path string) ([]string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	rel = rel[:len(rel)-len(filepath.Ext(rel))]
	rel = filepath.ToSlash(rel)

	var segs []string
	for _, part := range strings.Split(rel, "/") {
		for _, s := range strings.Split(part, ".") {
			if s != "" {
				segs = append(segs, s)
			}
		}
	}
	return segs, nil
}
