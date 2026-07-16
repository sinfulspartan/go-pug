package gopug_test

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"io"
)

func RenderBenchAttrs(w io.Writer, d BenchAttrsData) error {
	for _, item := range d.Items {
		_ = item
		io.WriteString(w, "<div")
		if err := gopug.WriteSpreadAttrs(w, map[string]*gopug.AttributeValue{"class": {Value: "row"}, "data-id": {Value: item.ID}}, item.Attrs); err != nil {
			return err
		}
		io.WriteString(w, "><span>")
		io.WriteString(w, gopug.EscapeHTML(item.ID))
		io.WriteString(w, "</span></div>")
	}
	return nil
}
