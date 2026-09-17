// Command yadb operates on a filesystem of YAML files arranged in nested
// directories, treating the directory layout as a single dotted namespace.
// A file at foo/bar/baz.yaml exposes its fields under "foo.bar.baz", so
// "foo.bar.baz.config.port" addresses ".config.port" inside that file.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"yadb/internal/store"

	"github.com/spf13/cobra"
)

var (
	rootDir string
	exts    []string
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "yadb:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "yadb",
		Short: "Query and edit a tree of YAML files as one dotted namespace",
		Long: `yadb treats a directory of YAML files as a single namespace.

A file at foo/bar/baz.yaml contributes its contents under the prefix
"foo.bar.baz", so the field "foo.bar.baz.config.port" resolves to the
".config.port" path inside that file.

Fields may use wildcards to address multiple files:
  *   matches exactly one path segment
  **  matches zero or more path segments

Examples:
  yadb get   foo.bar.baz.config.port      # query one value
  yadb get   '**.name'                     # every top-level name field
  yadb find  '**.replicas'                 # files that set replicas
  yadb find  'services.*.image=nginx'      # files where image == nginx
  yadb set   foo.bar.baz.config.port=9090  # set one value
  yadb set   '**.enabled=true'             # set across many files
  yadb keys                                 # top-level namespace segments
  yadb keys  foo.bar.baz.config             # keys available under a path`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&rootDir, "root", "C", ".", "root directory to scan")
	root.PersistentFlags().StringSliceVarP(&exts, "ext", "e", nil, "YAML file extensions (default .yaml,.yml)")

	root.AddCommand(newFindCmd(), newGetCmd(), newSetCmd(), newUnsetCmd(), newKeysCmd())
	return root
}

func loadFiles() ([]store.File, error) {
	return store.Scan(rootDir, exts)
}

func newKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys [<path>]",
		Short: "List the keys available directly under a path",
		Long: `keys lists the immediate children of a dotted path: the next namespace
segments contributed by directories and files below it, plus the map keys at
that path inside any matching files. With no argument it lists the top-level
segments of the namespace.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := loadFiles()
			if err != nil {
				return err
			}
			var arg string
			if len(args) == 1 {
				arg = args[0]
			}
			matches := store.Resolve(files, store.ParseField(arg))

			seen := map[string]bool{}
			var keys []string
			add := func(k string) {
				if k != "" && !seen[k] {
					seen[k] = true
					keys = append(keys, k)
				}
			}
			for _, m := range matches {
				if m.Ancestor {
					add(m.Remainder[0])
					continue
				}
				ks, err := store.Keys(m)
				if err != nil {
					return err
				}
				for _, k := range ks {
					add(k)
				}
			}
			if len(matches) == 0 {
				return fmt.Errorf("no file matched path %q", arg)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintln(cmd.OutOrStdout(), k)
			}
			return nil
		},
	}
	return cmd
}

func newFindCmd() *cobra.Command {
	var asString bool
	cmd := &cobra.Command{
		Use:   "find <field>[=<value>]",
		Short: "List files that have a field (optionally with a specific value)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := loadFiles()
			if err != nil {
				return err
			}
			fieldArg, value, hasValue := strings.Cut(args[0], "=")
			matches := store.Resolve(files, store.ParseField(fieldArg))

			seen := map[string]bool{}
			var out []string
			for _, m := range matches {
				var ok bool
				switch {
				case hasValue:
					if m.Ancestor {
						continue
					}
					ok, err = store.Equals(m, value, asString)
				case m.Ancestor:
					ok = true
				default:
					ok, err = store.Exists(m)
				}
				if err != nil {
					return err
				}
				if ok && !seen[m.File.Path] {
					seen[m.File.Path] = true
					out = append(out, m.File.Path)
				}
			}
			sort.Strings(out)
			for _, p := range out {
				fmt.Fprintln(cmd.OutOrStdout(), p)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&asString, "string", "s", false, "compare the value as a string")
	return cmd
}

func newGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <field>",
		Short: "Print the value of a field",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := loadFiles()
			if err != nil {
				return err
			}
			field := store.ParseField(args[0])
			candidates := store.InsideMatches(store.Resolve(files, field))
			if !field.Wildcard {
				if m, ok := store.Deepest(candidates); ok {
					candidates = []store.Match{m}
				} else {
					candidates = nil
				}
			}

			type result struct{ field, value string }
			var results []result
			for _, m := range candidates {
				present, err := store.Exists(m)
				if err != nil {
					return err
				}
				if !present {
					continue
				}
				value, err := store.Get(m)
				if err != nil {
					return err
				}
				results = append(results, result{m.ResolvedField(), value})
			}
			if len(results) == 0 {
				return fmt.Errorf("no value found for field %q", args[0])
			}
			sort.Slice(results, func(i, j int) bool { return results[i].field < results[j].field })

			w := cmd.OutOrStdout()
			if len(results) == 1 {
				fmt.Fprintln(w, results[0].value)
				return nil
			}
			for _, r := range results {
				if strings.Contains(r.value, "\n") {
					fmt.Fprintf(w, "%s:\n%s\n", r.field, indent(r.value))
				} else {
					fmt.Fprintf(w, "%s: %s\n", r.field, r.value)
				}
			}
			return nil
		},
	}
	return cmd
}

func newSetCmd() *cobra.Command {
	var asString, dryRun bool
	cmd := &cobra.Command{
		Use:   "set <field>=<value> [<field>=<value> ...]",
		Short: "Set the value of fields in one or more files",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := loadFiles()
			if err != nil {
				return err
			}
			// Collect all target matches first so a bad assignment fails before
			// any file is written.
			type assignment struct {
				match store.Match
				value string
			}
			var plan []assignment
			for _, arg := range args {
				fieldArg, value, ok := strings.Cut(arg, "=")
				if !ok {
					return fmt.Errorf("invalid assignment %q: expected field=value", arg)
				}
				field := store.ParseField(fieldArg)
				candidates := store.InsideMatches(store.Resolve(files, field))
				if !field.Wildcard {
					m, ok := store.Deepest(candidates)
					if !ok {
						return fmt.Errorf("no file matched field %q", fieldArg)
					}
					candidates = []store.Match{m}
				}
				if len(candidates) == 0 {
					return fmt.Errorf("no file matched field %q", fieldArg)
				}
				for _, m := range candidates {
					plan = append(plan, assignment{m, value})
				}
			}

			w := cmd.OutOrStdout()
			changed := map[string]bool{}
			for _, a := range plan {
				if dryRun {
					fmt.Fprintf(w, "would set %s = %s (%s)\n", a.match.ResolvedField(), a.value, a.match.File.Path)
					changed[a.match.File.Path] = true
					continue
				}
				if err := store.Set(a.match, a.value, asString); err != nil {
					return err
				}
				fmt.Fprintf(w, "set %s = %s (%s)\n", a.match.ResolvedField(), a.value, a.match.File.Path)
				changed[a.match.File.Path] = true
			}
			verb := "updated"
			if dryRun {
				verb = "would update"
			}
			fmt.Fprintf(w, "%s %d file(s)\n", verb, len(changed))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&asString, "string", "s", false, "set the value as a string even if it looks numeric/boolean")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing files")
	return cmd
}

func newUnsetCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:     "unset <field> [<field> ...]",
		Aliases: []string{"del", "delete"},
		Short:   "Delete fields from one or more files",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := loadFiles()
			if err != nil {
				return err
			}
			// Resolve every target first so a bad field fails before any write.
			var targets []store.Match
			for _, arg := range args {
				field := store.ParseField(arg)
				candidates := store.InsideMatches(store.Resolve(files, field))
				if !field.Wildcard {
					m, ok := store.Deepest(candidates)
					if !ok {
						return fmt.Errorf("no file matched field %q", arg)
					}
					candidates = []store.Match{m}
				}
				if len(candidates) == 0 {
					return fmt.Errorf("no file matched field %q", arg)
				}
				targets = append(targets, candidates...)
			}

			w := cmd.OutOrStdout()
			changed := map[string]bool{}
			for _, m := range targets {
				if dryRun {
					present, err := store.Present(m)
					if err != nil {
						return err
					}
					if present {
						fmt.Fprintf(w, "would delete %s (%s)\n", m.ResolvedField(), m.File.Path)
						changed[m.File.Path] = true
					}
					continue
				}
				existed, err := store.Unset(m)
				if err != nil {
					return err
				}
				if existed {
					fmt.Fprintf(w, "deleted %s (%s)\n", m.ResolvedField(), m.File.Path)
					changed[m.File.Path] = true
				}
			}
			verb := "updated"
			if dryRun {
				verb = "would update"
			}
			fmt.Fprintf(w, "%s %d file(s)\n", verb, len(changed))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing files")
	return cmd
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = "  " + l
		}
	}
	return strings.Join(lines, "\n")
}
