package gopug_test

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"io"
)

func RenderMedium(w io.Writer, d MediumData) error {
	io.WriteString(w, "<div id=\"")
	io.WriteString(w, gopug.EscapeAttr(d.CardId))
	io.WriteString(w, "\" class=\"card\"><h2>")
	io.WriteString(w, gopug.EscapeHTML(d.Title))
	io.WriteString(w, "</h2><p>")
	io.WriteString(w, gopug.EscapeHTML(d.Description))
	io.WriteString(w, "</p>")
	if gopug.Truthy(d.Badge) {
		io.WriteString(w, "<span class=\"badge\">")
		io.WriteString(w, gopug.EscapeHTML(d.Badge))
		io.WriteString(w, "</span>")
	}
	io.WriteString(w, "<a href=\"")
	io.WriteString(w, gopug.EscapeAttr(d.Url))
	io.WriteString(w, "\">Read more</a></div>")
	return nil
}
