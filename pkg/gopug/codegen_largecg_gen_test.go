package gopug_test

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"html"
	"io"
	"strconv"
)

func RenderLargeCG(w io.Writer, d LargeCGData) error {
	io.WriteString(w, "<!DOCTYPE html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>")
	io.WriteString(w, html.EscapeString(d.PageTitle))
	io.WriteString(w, "</title></head><body><header><nav><a href=\"/\">Home</a><a href=\"/about\">About</a></nav></header><main><h1>")
	io.WriteString(w, html.EscapeString(d.Heading))
	io.WriteString(w, "</h1><p>")
	io.WriteString(w, html.EscapeString(d.Intro))
	io.WriteString(w, "</p><ul class=\"items\">")
	for _, product := range d.Products {
		_ = product
		io.WriteString(w, "<li class=\"item\" data-id=\"")
		io.WriteString(w, gopug.EscapeAttr(strconv.Itoa(product.ID)))
		io.WriteString(w, "\"><span class=\"name\">")
		io.WriteString(w, html.EscapeString(product.Name))
		io.WriteString(w, "</span><span class=\"price\">")
		io.WriteString(w, html.EscapeString(product.Price))
		io.WriteString(w, "</span>")
		if product.OnSale {
			io.WriteString(w, "<span class=\"badge\">Sale</span>")
		}
		io.WriteString(w, "</li>")
	}
	io.WriteString(w, "</ul>")
	if d.ShowFootnote {
		io.WriteString(w, "<p class=\"footnote\">")
		io.WriteString(w, html.EscapeString(d.Footnote))
		io.WriteString(w, "</p>")
	}
	io.WriteString(w, "</main><footer><p>Go-Pug</p></footer></body></html>")
	return nil
}
