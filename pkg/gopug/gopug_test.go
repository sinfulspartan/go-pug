package gopug

import (
	"fmt"
	"testing"
)

func TestHello(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"", "Hello, world!"},
		{"Alice", "Hello, Alice!"},
		{"Bob", "Hello, Bob!"},
		{"Go-Pug", "Hello, Go-Pug!"},
	}

	for _, c := range cases {
		got := Hello(c.name)
		if got != c.want {
			t.Fatalf("Hello(%q) = %q; want %q", c.name, got, c.want)
		}
	}
}

// ExampleHello demonstrates basic usage of Hello.
//
// The output is checked by `go test`.
func ExampleHello() {
	fmt.Println(Hello("Go-Pug"))
	// Output: Hello, Go-Pug!
}
