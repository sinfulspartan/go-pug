package gopug_test

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"html"
	"io"
)

func RenderBenchMixin(w io.Writer, d BenchMixinData) error {
	for _, item := range d.Items {
		__marg0_0 := item.Title
		__marg0_1 := item.Kind
		if err := pugMixin_card(w, __marg0_0, __marg0_1, func(w io.Writer) error {
			io.WriteString(w, "<p>Standard item footer</p>")
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func pugMixin_card(w io.Writer, arg1 string, arg2 string, pugBlock func(io.Writer) error) error {
	io.WriteString(w, "<div class=\"card\" data-kind=\"")
	io.WriteString(w, gopug.EscapeAttr(arg2))
	io.WriteString(w, "\"><h3>")
	io.WriteString(w, html.EscapeString(arg1))
	io.WriteString(w, "</h3>")
	if pugBlock != nil {
		if err := pugBlock(w); err != nil {
			return err
		}
	}
	io.WriteString(w, "</div>")
	return nil
}
