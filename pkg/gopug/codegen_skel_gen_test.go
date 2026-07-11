package gopug

import (
	"html"
	"io"
)

func RenderSkel(w io.Writer, d SkelData) error {
	io.WriteString(w, "<!DOCTYPE html><html><head><title>Skeleton</title></head><body><div id=\"main\" class=\"container\" data-role=\"app\"><p>Hello, ")
	io.WriteString(w, html.EscapeString(d.Name))
	io.WriteString(w, "!</p><p>Bio: ")
	io.WriteString(w, html.EscapeString(d.Author.Bio))
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
	io.WriteString(w, "</div></body></html>")
	return nil
}
