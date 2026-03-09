// gofmt applies gofmt -s (simplification + formatting) to every .go file in
// the module tree, mirroring what `gofmt -s -l -w .` does on the command line
// but using only the Go standard library — no external tools required.
//
// Usage (via Makefile):
//
//	go run ./scripts/gofmt
//
// Flags:
//
//	-l   list files that would be changed without writing them (dry-run)
//	-d   print a unified diff for each changed file (dry-run)
//
// Without flags the script rewrites files in-place and prints the name of
// every file that was changed, matching the behaviour of `gofmt -s -l -w .`.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

func main() {
	listOnly := flag.Bool("l", false, "list files whose formatting differs (do not write)")
	showDiff := flag.Bool("d", false, "display diffs instead of rewriting files")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: go run ./scripts/gofmt [-l] [-d]")
		fmt.Fprintln(os.Stderr, "Applies gofmt -s to every .go file in the repository.")
		flag.PrintDefaults()
	}
	flag.Parse()

	root := "."

	var changed []string
	var errs []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", path, err))
			return nil
		}

		// Skip hidden directories (e.g. .git, .agent) and vendor.
		if d.IsDir() {
			name := d.Name()
			if name != "." && (strings.HasPrefix(name, ".") || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: read: %v", path, err))
			return nil
		}

		formatted, err := formatSource(path, src)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", path, err))
			return nil
		}

		if bytes.Equal(src, formatted) {
			return nil
		}

		changed = append(changed, path)

		switch {
		case *showDiff:
			printDiff(path, src, formatted)
		case *listOnly:
			fmt.Println(path)
		default:
			if err := os.WriteFile(path, formatted, 0644); err != nil {
				errs = append(errs, fmt.Sprintf("%s: write: %v", path, err))
				return nil
			}
			fmt.Println(path)
		}

		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "gofmt: walk error: %v\n", err)
		os.Exit(1)
	}

	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "gofmt: %s\n", e)
	}
	if len(errs) > 0 {
		os.Exit(1)
	}

	// Non-zero exit when -l or -d finds files that need formatting so CI can
	// use `go run ./scripts/gofmt -l` as a formatting gate.
	if (*listOnly || *showDiff) && len(changed) > 0 {
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

// formatSource parses src as a Go source file, applies -s simplifications,
// and returns the canonical gofmt output.
func formatSource(filename string, src []byte) ([]byte, error) {
	fset := token.NewFileSet()

	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	simplify(f)

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// AST simplifier  (mirrors gofmt -s)
// ---------------------------------------------------------------------------
// gofmt -s applies three source-level simplifications:
//
//  1. Composite literal key elision
//     []T{T{...}, T{...}}  →  []T{{...}, {...}}
//
//  2. Slice expression simplification
//     s[a:len(s)]  →  s[a:]
//
//  3. Range variable elision
//     for x, _ = range v  →  for x = range v
//     for _, _ = range v  →  for range v

func simplify(f *ast.File) {
	var s simplifier
	ast.Walk(&s, f)
}

type simplifier struct{}

func (s *simplifier) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.CompositeLit:
		simplifyCompositeLit(n)
	case *ast.SliceExpr:
		simplifySliceExpr(n)
	case *ast.RangeStmt:
		simplifyRangeStmt(n)
	}
	return s
}

// simplifyCompositeLit removes redundant type keys from composite literals
// whose element type matches the outer literal's element type.
//
//	[]Point{Point{1, 2}, Point{3, 4}}  →  []Point{{1, 2}, {3, 4}}
func simplifyCompositeLit(lit *ast.CompositeLit) {
	var eltType ast.Expr
	switch t := lit.Type.(type) {
	case *ast.ArrayType:
		eltType = t.Elt
	case *ast.MapType:
		eltType = t.Value
	default:
		return
	}
	if eltType == nil {
		return
	}

	for _, elt := range lit.Elts {
		kv, isKV := elt.(*ast.KeyValueExpr)
		var inner *ast.CompositeLit
		if isKV {
			inner, _ = kv.Value.(*ast.CompositeLit)
		} else {
			inner, _ = elt.(*ast.CompositeLit)
		}
		if inner == nil || inner.Type == nil {
			continue
		}
		if reflect.DeepEqual(inner.Type, eltType) {
			inner.Type = nil
		}
	}
}

// simplifySliceExpr rewrites s[a:len(s)] → s[a:].
func simplifySliceExpr(s *ast.SliceExpr) {
	if s.High == nil || s.Slice3 {
		return
	}
	call, ok := s.High.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || call.Ellipsis.IsValid() {
		return
	}
	fn, ok := call.Fun.(*ast.Ident)
	if !ok || fn.Name != "len" {
		return
	}
	if reflect.DeepEqual(s.X, call.Args[0]) {
		s.High = nil
	}
}

// simplifyRangeStmt removes unnecessary blank-identifier loop variables.
//
//	for x, _ = range v  →  for x = range v
//	for _, _ = range v  →  for range v
func simplifyRangeStmt(r *ast.RangeStmt) {
	if r.Value == nil {
		return
	}
	ident, ok := r.Value.(*ast.Ident)
	if !ok || ident.Name != "_" {
		return
	}
	r.Value = nil
	// If the key was also blank, drop it too to get bare `for range`.
	if r.Key != nil {
		if ki, ok := r.Key.(*ast.Ident); ok && ki.Name == "_" {
			r.Key = nil
		}
	}
}

// ---------------------------------------------------------------------------
// Minimal unified diff
// ---------------------------------------------------------------------------

func printDiff(filename string, old, new []byte) {
	oldLines := strings.Split(string(old), "\n")
	newLines := strings.Split(string(new), "\n")

	fmt.Printf("--- %s (original)\n", filename)
	fmt.Printf("+++ %s (formatted)\n", filename)

	for _, h := range computeDiff(oldLines, newLines) {
		fmt.Print(h)
	}
}

// computeDiff returns unified-diff hunk strings for old → new using LCS.
func computeDiff(old, new []string) []string {
	m, n := len(old), len(new)

	// Build LCS table.
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if old[i] == new[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] > dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	// Walk table to produce edit ops.
	type op struct {
		kind rune // '=' keep, '-' delete, '+' insert
		line string
		oldN int
		newN int
	}
	var ops []op
	i, j := 0, 0
	for i < m || j < n {
		switch {
		case i < m && j < n && old[i] == new[j]:
			ops = append(ops, op{'=', old[i], i + 1, j + 1})
			i++
			j++
		case j < n && (i >= m || dp[i][j+1] >= dp[i+1][j]):
			ops = append(ops, op{'+', new[j], i + 1, j + 1})
			j++
		default:
			ops = append(ops, op{'-', old[i], i + 1, j + 1})
			i++
		}
	}

	// Group into hunks with 3-line context.
	const ctx = 3
	var hunks []string
	total := len(ops)
	idx := 0
	for idx < total {
		if ops[idx].kind == '=' {
			idx++
			continue
		}

		start := idx - ctx
		if start < 0 {
			start = 0
		}

		end := idx
		for end < total {
			if ops[end].kind != '=' {
				end++
				continue
			}
			eq := 0
			for k := end; k < total && ops[k].kind == '='; k++ {
				eq++
			}
			if eq > ctx*2 {
				break
			}
			end += eq
		}
		end += ctx
		if end > total {
			end = total
		}

		oldStart, oldCount := -1, 0
		newStart, newCount := -1, 0
		for _, o := range ops[start:end] {
			if o.kind != '+' {
				if oldStart < 0 {
					oldStart = o.oldN
				}
				oldCount++
			}
			if o.kind != '-' {
				if newStart < 0 {
					newStart = o.newN
				}
				newCount++
			}
		}
		if oldStart < 0 {
			oldStart = 1
		}
		if newStart < 0 {
			newStart = 1
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for _, o := range ops[start:end] {
			switch o.kind {
			case '=':
				fmt.Fprintf(&sb, " %s\n", o.line)
			case '-':
				fmt.Fprintf(&sb, "-%s\n", o.line)
			case '+':
				fmt.Fprintf(&sb, "+%s\n", o.line)
			}
		}
		hunks = append(hunks, sb.String())
		idx = end
	}
	return hunks
}
