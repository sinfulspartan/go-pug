package gopug_test

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"io"
	"strconv"
	"unicode/utf8"
)

func RenderSkel(w io.Writer, d SkelData) error {
	io.WriteString(w, "<!DOCTYPE html><html><head><title>Skeleton</title></head><body><div id=\"main\" class=\"container\" data-role=\"app\"><p>Hello, ")
	io.WriteString(w, gopug.EscapeHTML(d.Name))
	io.WriteString(w, "!</p><p>Bio: ")
	io.WriteString(w, gopug.EscapeHTML(d.Author.Bio))
	io.WriteString(w, "</p><p>Count: ")
	io.WriteString(w, gopug.EscapeHTML(strconv.Itoa(d.Count)))
	io.WriteString(w, "</p><p>Price: ")
	io.WriteString(w, gopug.EscapeHTML(strconv.FormatFloat(d.Price, 'f', -1, 64)))
	io.WriteString(w, "</p><p>Flag: ")
	io.WriteString(w, gopug.EscapeHTML(strconv.FormatBool(d.Flag)))
	io.WriteString(w, "</p><img alt=\"logo\" src=\"/logo.png\"><a id=\"profile\" href=\"")
	io.WriteString(w, gopug.EscapeAttr(d.Link))
	io.WriteString(w, "\">Profile</a><img src=\"")
	io.WriteString(w, gopug.EscapeAttr(d.Author.Avatar))
	io.WriteString(w, "\"><div data-count=\"")
	io.WriteString(w, gopug.EscapeAttr(strconv.Itoa(d.Count)))
	io.WriteString(w, "\">Count attr</div><input")
	if d.Flag {
		io.WriteString(w, " checked=\"true\"")
	}
	io.WriteString(w, "><div data-flag=\"")
	io.WriteString(w, gopug.EscapeAttr(strconv.FormatBool(d.Flag)))
	io.WriteString(w, "\">Flag attr</div><div class=\"")
	io.WriteString(w, gopug.EscapeAttr(gopug.JoinClasses("card", d.Variant)))
	io.WriteString(w, "\">Card</div><span class=\"")
	io.WriteString(w, gopug.EscapeAttr(gopug.JoinClasses(d.Extra)))
	io.WriteString(w, "\">Extra</span><br><ul>")
	for _, item := range d.Items {
		_ = item
		io.WriteString(w, "<li>")
		io.WriteString(w, gopug.EscapeHTML(item.Label))
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
	if len(d.Items) > 0 {
		io.WriteString(w, "<p class=\"has-items\">Has items (length)</p>")
	} else {
		io.WriteString(w, "<p class=\"no-items\">No items (length)</p>")
	}
	if d.Count > 0 {
		io.WriteString(w, "<p class=\"positive\">Positive</p>")
	} else {
		io.WriteString(w, "<p class=\"not-positive\">Not positive</p>")
	}
	if d.Count == 3 {
		io.WriteString(w, "<p class=\"three\">Three</p>")
	} else {
		io.WriteString(w, "<p class=\"not-three\">Not three</p>")
	}
	if d.Name == "world" {
		io.WriteString(w, "<p class=\"name-world\">Name is world</p>")
	} else {
		io.WriteString(w, "<p class=\"name-other\">Name is other</p>")
	}
	if utf8.RuneCountInString(d.Name) > 2 {
		io.WriteString(w, "<p class=\"long-name\">Long name</p>")
	} else {
		io.WriteString(w, "<p class=\"short-name\">Short name</p>")
	}
	io.WriteString(w, "</div></body></html>")
	return nil
}
