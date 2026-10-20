// Command doclint checks Markdown files against the xis writing rules
// (about 80% ASD-STE100). It reports long sentences, common passive
// patterns and non-preferred words. It only warns: the exit code is 0
// unless you give -strict.
//
// Usage: go run ./internal/tools/doclint [-strict] FILE_OR_DIR...
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	strict := flag.Bool("strict", false, "exit 1 if there are warnings")
	flag.Parse()
	paths := flag.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	n, err := run(os.Stdout, paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "doclint:", err)
		os.Exit(2)
	}
	if n > 0 {
		fmt.Fprintf(os.Stdout, "doclint: %d warnings\n", n)
		if *strict {
			os.Exit(1)
		}
	}
}

func run(w io.Writer, paths []string) (int, error) {
	files, err := collect(paths)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return total, err
		}
		for _, warn := range Check(string(b)) {
			fmt.Fprintf(w, "%s:%d: %s\n", f, warn.Line, warn.Msg)
			total++
		}
	}
	return total, nil
}

func collect(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			out = append(out, p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if path != p && (strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules" || name == "dist") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".md") && !strings.EqualFold(filepath.Base(path), "Spec.md") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}
