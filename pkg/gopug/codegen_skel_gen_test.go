package gopug_test

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"io"
	"strconv"
	"unicode/utf8"
)

func RenderSkel(w io.Writer, d SkelData) error {
	sw := gopug.StringWriter(w)
	sw.WriteString("<!DOCTYPE html><html><head><title>Skeleton</title></head><body><div id=\"main\" class=\"container\" data-role=\"app\"><p>Hello, ")
	sw.WriteString(gopug.EscapeHTML(d.Name))
	sw.WriteString("!</p><p>Bio: ")
	sw.WriteString(gopug.EscapeHTML(d.Author.Bio))
	sw.WriteString("</p><p>Count: ")
	sw.WriteString(gopug.EscapeHTML(strconv.Itoa(d.Count)))
	sw.WriteString("</p><p>Price: ")
	sw.WriteString(gopug.EscapeHTML(strconv.FormatFloat(d.Price, 'f', -1, 64)))
	sw.WriteString("</p><p>Flag: ")
	sw.WriteString(gopug.EscapeHTML(strconv.FormatBool(d.Flag)))
	sw.WriteString("</p><img alt=\"logo\" src=\"/logo.png\"><a id=\"profile\" href=\"")
	sw.WriteString(gopug.EscapeAttr(d.Link))
	sw.WriteString("\">Profile</a><img src=\"")
	sw.WriteString(gopug.EscapeAttr(d.Author.Avatar))
	sw.WriteString("\"><div data-count=\"")
	sw.WriteString(gopug.EscapeAttr(strconv.Itoa(d.Count)))
	sw.WriteString("\">Count attr</div><input")
	if d.Flag {
		sw.WriteString(" checked=\"true\"")
	}
	sw.WriteString("><div data-flag=\"")
	sw.WriteString(gopug.EscapeAttr(strconv.FormatBool(d.Flag)))
	sw.WriteString("\">Flag attr</div><div class=\"")
	sw.WriteString(gopug.EscapeAttr(gopug.JoinClasses("card", d.Variant)))
	sw.WriteString("\">Card</div><span class=\"")
	sw.WriteString(gopug.EscapeAttr(gopug.JoinClasses(d.Extra)))
	sw.WriteString("\">Extra</span><br><ul>")
	for _, item := range d.Items {
		_ = item
		sw.WriteString("<li>")
		sw.WriteString(gopug.EscapeHTML(item.Label))
		sw.WriteString("</li>")
	}
	sw.WriteString("</ul>")
	if d.Flag {
		sw.WriteString("<p class=\"active\">On</p>")
	} else {
		sw.WriteString("<p class=\"inactive\">Off</p>")
	}
	if d.Count != 0 {
		sw.WriteString("<p class=\"has-count\">Has items</p>")
	} else {
		sw.WriteString("<p class=\"no-count\">No items</p>")
	}
	if len(d.Items) > 0 {
		sw.WriteString("<p class=\"has-items\">Has items (length)</p>")
	} else {
		sw.WriteString("<p class=\"no-items\">No items (length)</p>")
	}
	if d.Count > 0 {
		sw.WriteString("<p class=\"positive\">Positive</p>")
	} else {
		sw.WriteString("<p class=\"not-positive\">Not positive</p>")
	}
	if d.Count == 3 {
		sw.WriteString("<p class=\"three\">Three</p>")
	} else {
		sw.WriteString("<p class=\"not-three\">Not three</p>")
	}
	if d.Name == "world" {
		sw.WriteString("<p class=\"name-world\">Name is world</p>")
	} else {
		sw.WriteString("<p class=\"name-other\">Name is other</p>")
	}
	if utf8.RuneCountInString(d.Name) > 2 {
		sw.WriteString("<p class=\"long-name\">Long name</p>")
	} else {
		sw.WriteString("<p class=\"short-name\">Short name</p>")
	}
	sw.WriteString("</div></body></html>")
	return nil
}
