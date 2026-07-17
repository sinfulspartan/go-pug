package gopug_test

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"io"
)

func RenderBenchAttrs(w io.Writer, d BenchAttrsData) error {
	sw := gopug.StringWriter(w)
	for _, item := range d.Items {
		_ = item
		sw.WriteString("<div")
		if err := gopug.WriteSpreadAttrs(w, map[string]*gopug.AttributeValue{"class": {Value: "row"}, "data-id": {Value: item.ID}}, item.Attrs); err != nil {
			return err
		}
		sw.WriteString("><span>")
		sw.WriteString(gopug.EscapeHTML(item.ID))
		sw.WriteString("</span></div>")
	}
	return nil
}
