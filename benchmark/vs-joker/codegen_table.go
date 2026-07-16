package main

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"io"
	"strings"
)

func CGTable(w io.Writer, d tableData) error {
	io.WriteString(w, "<!DOCTYPE html><html><head><title>")
	io.WriteString(w, gopug.EscapeHTML(d.PageTitle))
	io.WriteString(w, "</title></head><body><h1>")
	io.WriteString(w, gopug.EscapeHTML(d.PageTitle))
	io.WriteString(w, "</h1><table class=\"data-table\"><thead><tr><th>Name</th><th>Department</th><th>Status</th><th>Salary</th></tr></thead><tbody>")
	for _, employee := range d.Employees {
		_ = employee
		io.WriteString(w, "<tr")
		var __cls0 []string
		if true {
			__cls0 = append(__cls0, "data-table__row")
		}
		if employee.Highlight {
			__cls0 = append(__cls0, "data-table__row--highlight")
		}
		__clsStr1 := strings.Join(__cls0, " ")
		io.WriteString(w, " class=\"")
		io.WriteString(w, gopug.EscapeAttr(__clsStr1))
		io.WriteString(w, "\"><td>")
		io.WriteString(w, gopug.EscapeHTML(employee.Name))
		io.WriteString(w, "</td><td>")
		io.WriteString(w, gopug.EscapeHTML(employee.Department))
		io.WriteString(w, "</td><td>")
		io.WriteString(w, gopug.EscapeHTML(employee.Status))
		io.WriteString(w, "</td><td>")
		io.WriteString(w, gopug.EscapeHTML(employee.Salary))
		io.WriteString(w, "</td></tr>")
	}
	io.WriteString(w, "</tbody></table></body></html>")
	return nil
}
