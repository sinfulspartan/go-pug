package gopug_test

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"io"
)

func RenderBenchMixin(w io.Writer, d BenchMixinData) error {
	for _, item := range d.Items {
		_ = item
		__marg0_0 := item.Title
		__marg0_1 := item.Kind
		if err := pugMixin_card(w, __marg0_0, __marg0_1, func(w io.Writer) error {
			sw := gopug.StringWriter(w)
			sw.WriteString("<p>Standard item footer</p>")
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func pugMixin_card(w io.Writer, arg1 string, arg2 string, pugBlock func(io.Writer) error) error {
	sw := gopug.StringWriter(w)
	sw.WriteString("<div class=\"card\" data-kind=\"")
	sw.WriteString(gopug.EscapeAttr(arg2))
	sw.WriteString("\"><h3>")
	sw.WriteString(gopug.EscapeHTML(arg1))
	sw.WriteString("</h3>")
	if pugBlock != nil {
		if err := pugBlock(w); err != nil {
			return err
		}
	}
	sw.WriteString("</div>")
	return nil
}
