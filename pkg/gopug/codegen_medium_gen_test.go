package gopug_test

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"io"
)

func RenderMedium(w io.Writer, d MediumData) error {
	sw := gopug.StringWriter(w)
	sw.WriteString("<div id=\"")
	sw.WriteString(gopug.EscapeAttr(d.CardId))
	sw.WriteString("\" class=\"card\"><h2>")
	sw.WriteString(gopug.EscapeHTML(d.Title))
	sw.WriteString("</h2><p>")
	sw.WriteString(gopug.EscapeHTML(d.Description))
	sw.WriteString("</p>")
	if gopug.Truthy(d.Badge) {
		sw.WriteString("<span class=\"badge\">")
		sw.WriteString(gopug.EscapeHTML(d.Badge))
		sw.WriteString("</span>")
	}
	sw.WriteString("<a href=\"")
	sw.WriteString(gopug.EscapeAttr(d.Url))
	sw.WriteString("\">Read more</a></div>")
	return nil
}
