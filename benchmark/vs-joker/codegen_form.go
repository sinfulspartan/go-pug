package main

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"io"
)

func CGForm(w io.Writer, d formData) error {
	io.WriteString(w, "<!DOCTYPE html><html><head><title>Account Settings</title></head><body><h1>Account Settings</h1><form action=\"/settings\" method=\"post\">")
	for _, field := range d.Fields {
		_ = field
		io.WriteString(w, "<div class=\"form-group\"><label for=\"")
		io.WriteString(w, gopug.EscapeAttr(field.ID))
		io.WriteString(w, "\">")
		io.WriteString(w, gopug.EscapeHTML(field.Label))
		io.WriteString(w, "</label><input id=\"")
		io.WriteString(w, gopug.EscapeAttr(field.ID))
		io.WriteString(w, "\" name=\"")
		io.WriteString(w, gopug.EscapeAttr(field.ID))
		io.WriteString(w, "\" type=\"text\" value=\"")
		io.WriteString(w, gopug.EscapeAttr(field.Value))
		io.WriteString(w, "\"></div>")
	}
	io.WriteString(w, "<div class=\"form-group\"><label for=\"role\">Role</label><select id=\"role\" name=\"role\">")
	for _, option := range d.RoleOptions {
		_ = option
		io.WriteString(w, "<option value=\"")
		io.WriteString(w, gopug.EscapeAttr(option.Value))
		io.WriteString(w, "\">")
		io.WriteString(w, gopug.EscapeHTML(option.Label))
		io.WriteString(w, "</option>")
	}
	io.WriteString(w, "</select></div><button type=\"submit\">Save Changes</button></form></body></html>")
	return nil
}
