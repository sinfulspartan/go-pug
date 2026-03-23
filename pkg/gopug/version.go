package gopug

// Version is the current release version of go-pug.
// It is set at source and mirrors the most-recently pushed git tag (e.g. v0.1.0).
// Consumers can read it at runtime:
//
//	fmt.Println(gopug.Version) // "v0.1.0"
//
// In release builds the value is overridden at link time via:
//
//	-ldflags "-X github.com/sinfulspartan/go-pug/pkg/gopug.Version=vX.Y.Z"
//
// so the binary always reports the exact tag that was built from.
var Version = "v0.1.2"
