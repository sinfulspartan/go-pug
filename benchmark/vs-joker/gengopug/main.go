// Command gengopug regenerates the go-pug codegen legs of the vs-joker
// comparison: it runs gopug.GenerateGo once per template against
// ../../templates/*.pug and writes the resulting typed Render functions into
// the parent directory as codegen_*.go, alongside the hand-maintained
// joker_*.go files the jade CLI produces. It has nothing to do with the
// benchmark's timed loop — this is the one-time "codegen" step, run at
// development time (like a go:generate), not inside main.go.
//
// Usage, from benchmark/vs-joker:
//
//	go run ./gengopug
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"

	"github.com/sinfulspartan/go-pug/pkg/gopug"
)

type product struct {
	Name        string
	Description string
	Price       string
	InStock     bool
	Featured    bool
}
type cardListData struct {
	PageTitle string
	Products  []product
}

type employee struct {
	Name       string
	Department string
	Status     string
	Salary     string
	Highlight  bool
}
type tableData struct {
	PageTitle string
	Employees []employee
}

type fieldSpec struct{ ID, Label, Value string }
type roleOption struct{ Value, Label string }
type formData struct {
	Fields      []fieldSpec
	RoleOptions []roleOption
}

type post struct{ Title, Author, Category, Summary string }
type blogData struct {
	PageTitle string
	Posts     []post
}

type target struct {
	template    string
	funcName    string
	dataType    string
	outfile     string
	reflectType reflect.Type
}

func generate(templatesDir, outDir string, t target) error {
	src, err := os.ReadFile(filepath.Join(templatesDir, t.template))
	if err != nil {
		return fmt.Errorf("reading %s: %w", t.template, err)
	}
	ast, err := gopug.Parse(string(src), &gopug.Options{Basedir: templatesDir})
	if err != nil {
		return fmt.Errorf("%s: parse: %w", t.template, err)
	}
	resolved, err := gopug.ResolveComposition(ast, &gopug.Options{Basedir: templatesDir})
	if err != nil {
		return fmt.Errorf("%s: resolve composition: %w", t.template, err)
	}
	out, err := gopug.GenerateGo(resolved, gopug.Config{
		PackageName:     "main",
		FuncName:        t.funcName,
		DataType:        t.dataType,
		DataReflectType: t.reflectType,
	})
	if err != nil {
		return fmt.Errorf("%s: generate: %w", t.template, err)
	}
	return os.WriteFile(filepath.Join(outDir, t.outfile), out, 0o644)
}

func main() {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "gengopug: could not determine this file's own path")
		os.Exit(1)
	}
	moduleDir := filepath.Dir(filepath.Dir(thisFile)) // benchmark/vs-joker
	templatesDir := filepath.Join(filepath.Dir(moduleDir), "templates")

	targets := []target{
		{"card_list.pug", "CGCardList", "cardListData", "codegen_card_list.go", reflect.TypeOf(cardListData{})},
		{"table.pug", "CGTable", "tableData", "codegen_table.go", reflect.TypeOf(tableData{})},
		{"form.pug", "CGForm", "formData", "codegen_form.go", reflect.TypeOf(formData{})},
		{"blog.pug", "CGBlog", "blogData", "codegen_blog.go", reflect.TypeOf(blogData{})},
	}

	for _, t := range targets {
		if err := generate(templatesDir, moduleDir, t); err != nil {
			fmt.Fprintln(os.Stderr, "gengopug:", err)
			os.Exit(1)
		}
		fmt.Println("generated:", t.outfile)
	}
}
