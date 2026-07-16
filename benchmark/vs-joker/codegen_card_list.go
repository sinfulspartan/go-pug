package main

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"io"
)

func CGCardList(w io.Writer, d cardListData) error {
	io.WriteString(w, "<!DOCTYPE html><html><head><title>")
	io.WriteString(w, gopug.EscapeHTML(d.PageTitle))
	io.WriteString(w, "</title></head><body><h1>")
	io.WriteString(w, gopug.EscapeHTML(d.PageTitle))
	io.WriteString(w, "</h1><div class=\"product-grid\">")
	for _, product := range d.Products {
		_ = product
		io.WriteString(w, "<div class=\"")
		io.WriteString(w, gopug.EscapeAttr(func() string {
			if product.Featured {
				return "product-card product-card--featured"
			}
			return "product-card"
		}()))
		io.WriteString(w, "\"><h2 class=\"product-card__name\">")
		io.WriteString(w, gopug.EscapeHTML(product.Name))
		io.WriteString(w, "</h2><p class=\"product-card__description\">")
		io.WriteString(w, gopug.EscapeHTML(product.Description))
		io.WriteString(w, "</p><p class=\"product-card__price\">")
		io.WriteString(w, gopug.EscapeHTML(product.Price))
		io.WriteString(w, "</p>")
		if product.InStock {
			io.WriteString(w, "<span class=\"badge badge--in-stock\">In stock</span>")
		} else {
			io.WriteString(w, "<span class=\"badge badge--out-of-stock\">Out of stock</span>")
		}
		io.WriteString(w, "</div>")
	}
	io.WriteString(w, "</div></body></html>")
	return nil
}
