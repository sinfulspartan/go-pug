package gopug

import (
	"html"
	"io"
	"strconv"
)

func RenderSkel(w io.Writer, d SkelData) error {
	io.WriteString(w, "<!DOCTYPE html><html><head><title>Skeleton</title></head><body><div id=\"main\" class=\"container\" data-role=\"app\"><p>Hello, ")
	io.WriteString(w, html.EscapeString(d.Name))
	io.WriteString(w, "!</p><p>Bio: ")
	io.WriteString(w, html.EscapeString(d.Author.Bio))
	io.WriteString(w, "</p><p>Count: ")
	io.WriteString(w, strconv.Itoa(d.Count))
	io.WriteString(w, "</p><p>Price: ")
	io.WriteString(w, strconv.FormatFloat(d.Price, 'f', -1, 64))
	io.WriteString(w, "</p><p>Flag: ")
	io.WriteString(w, strconv.FormatBool(d.Flag))
	io.WriteString(w, "</p><img alt=\"logo\" src=\"/logo.png\"><br><ul>")
	for _, item := range d.Items {
		io.WriteString(w, "<li>")
		io.WriteString(w, html.EscapeString(item.Label))
		io.WriteString(w, "</li>")
	}
	io.WriteString(w, "</ul>")
	if d.Flag {
		io.WriteString(w, "<p class=\"active\">On</p>")
	} else {
		io.WriteString(w, "<p class=\"inactive\">Off</p>")
	}
	if d.Count != 0 {
		io.WriteString(w, "<p class=\"has-count\">Has items</p>")
	} else {
		io.WriteString(w, "<p class=\"no-count\">No items</p>")
	}
	io.WriteString(w, "</div></body></html>")
	return nil
}
