// Package gopug provides a tiny, opinionated greeting utility intended
// to be used as a reusable library by other projects.
//
// The API is small and safe for use in other packages: it exposes a
// convenience function `Hello` plus a `Greeter` type for configurable use.
package gopug

import (
	"fmt"
	"strings"
)

// Version is the library semantic version. Increment when you make a release.
const Version = "0.1.0"

// Greeter is a configurable greeting generator. Users can set a custom
// Prefix which will be placed before the name in the greeting.
type Greeter struct {
	// Prefix placed before the name in the greeting. Default: "Hello"
	Prefix string
}

// New returns a Greeter configured with the provided prefix.
// If prefix is empty, it defaults to "Hello".
func New(prefix string) *Greeter {
	if strings.TrimSpace(prefix) == "" {
		prefix = "Hello"
	}
	return &Greeter{Prefix: prefix}
}

// Hello returns a greeting for the provided name using the Greeter's Prefix.
// If name is empty or only whitespace, a generic greeting is returned.
func (g *Greeter) Hello(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Sprintf("%s, world!", g.Prefix)
	}
	return fmt.Sprintf("%s, %s!", g.Prefix, titleName(name))
}

// Hello is a convenience function that uses a default Greeter with prefix "Hello".
func Hello(name string) string {
	return New("Hello").Hello(name)
}

// titleName applies light normalization to a name so greetings look tidy.
// It capitalizes the first rune of each word and collapses multiple spaces.
func titleName(s string) string {
	// Collapse multiple spaces and trim
	fields := strings.Fields(s)
	for i, f := range fields {
		// Use strings.Title-like behavior for ASCII-friendly names.
		// Avoid deprecated strings.Title; do simple capitalize.
		if f == "" {
			continue
		}
		runes := []rune(f)
		if len(runes) == 0 {
			continue
		}
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		// Lowercase rest (simple approach)
		if len(runes) > 1 {
			rest := strings.ToLower(string(runes[1:]))
			fields[i] = string(runes[0]) + rest
		} else {
			fields[i] = string(runes[0])
		}
	}
	return strings.Join(fields, " ")
}
