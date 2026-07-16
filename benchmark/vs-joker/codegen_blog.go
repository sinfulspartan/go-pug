package main

import (
	"github.com/sinfulspartan/go-pug/pkg/gopug"
	"io"
)

func CGBlog(w io.Writer, d blogData) error {
	io.WriteString(w, "<!DOCTYPE html><html><head><title>")
	io.WriteString(w, gopug.EscapeHTML(d.PageTitle))
	io.WriteString(w, "</title></head><body><h1>")
	io.WriteString(w, gopug.EscapeHTML(d.PageTitle))
	io.WriteString(w, "</h1>")
	for _, post := range d.Posts {
		_ = post
		io.WriteString(w, "<article class=\"post\"><h2>")
		io.WriteString(w, gopug.EscapeHTML(post.Title))
		io.WriteString(w, "</h2><p class=\"post-meta\">")
		io.WriteString(w, gopug.EscapeHTML(post.Author))
		io.WriteString(w, "</p>")
		__caseVal0 := post.Category
		__matched1 := false
		__done2 := false
		if __caseVal0 == "tech" {
			__matched1 = true
		}
		if __matched1 && !__done2 {
			io.WriteString(w, "<span class=\"tag tag--tech\">Tech</span>")
			__done2 = true
		}
		if __caseVal0 == "life" {
			__matched1 = true
		}
		if __matched1 && !__done2 {
			io.WriteString(w, "<span class=\"tag tag--life\">Life</span>")
			__done2 = true
		}
		if !__done2 {
			io.WriteString(w, "<span class=\"tag tag--general\">General</span>")
		}
		io.WriteString(w, "<p>")
		io.WriteString(w, gopug.EscapeHTML(post.Summary))
		io.WriteString(w, "</p></article>")
	}
	io.WriteString(w, "</body></html>")
	return nil
}
