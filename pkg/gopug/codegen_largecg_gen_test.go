package gopug_test

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"io"
	"strconv"
)

func RenderLargeCG(w io.Writer, d LargeCGData) error {
	sw := gopug.StringWriter(w)
	sw.WriteString("<!DOCTYPE html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>")
	sw.WriteString(gopug.EscapeHTML(d.PageTitle))
	sw.WriteString("</title></head><body><header><nav><a href=\"/\">Home</a><a href=\"/about\">About</a></nav></header><main><h1>")
	sw.WriteString(gopug.EscapeHTML(d.Heading))
	sw.WriteString("</h1><p>")
	sw.WriteString(gopug.EscapeHTML(d.Intro))
	sw.WriteString("</p><ul class=\"items\">")
	for _, product := range d.Products {
		_ = product
		sw.WriteString("<li class=\"item\" data-id=\"")
		sw.WriteString(gopug.EscapeAttr(strconv.Itoa(product.ID)))
		sw.WriteString("\"><span class=\"name\">")
		sw.WriteString(gopug.EscapeHTML(product.Name))
		sw.WriteString("</span><span class=\"price\">")
		sw.WriteString(gopug.EscapeHTML(product.Price))
		sw.WriteString("</span>")
		if product.OnSale {
			sw.WriteString("<span class=\"badge\">Sale</span>")
		}
		sw.WriteString("</li>")
	}
	sw.WriteString("</ul>")
	if d.ShowFootnote {
		sw.WriteString("<p class=\"footnote\">")
		sw.WriteString(gopug.EscapeHTML(d.Footnote))
		sw.WriteString("</p>")
	}
	sw.WriteString("</main><footer><p>Go-Pug</p></footer></body></html>")
	return nil
}
