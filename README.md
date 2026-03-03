# Go-Pug

Lightweight Go utility package scaffolded as a reusable module.

This repository is intended to be used as a library dependency by other Go projects. It includes a small example in `cmd/example` and a sample package under `pkg/gopug`.

## Quick start

1. Add the module to your project (replace `latest` with a version or commit tag when available):

```go
// in your module
go get github.com/sinfulspartan/go-pug@latest
```

2. Import and use the package in your code:

```go
import "github.com/sinfulspartan/go-pug/pkg/gopug"

func main() {
    // Example: call an exported function from the package
    result := gopug.Hello("world")
    fmt.Println(result)
}
```

3. Run the example program (from this repo root):

```sh
go run ./cmd/example
```

## Development

Common Makefile targets (available in the scaffolded `Makefile`):

- `make build` — build packages
- `make test` — run unit tests
- `make fmt` — format code with `gofmt`
- `make lint` — run linters (if configured)
- `make clean` — remove build artifacts

Or run the Go commands directly:

```sh
go test ./...
go vet ./...
go fmt ./...
```

## Contributing

Contributions are welcome. Please open issues or pull requests. Keep changes small and add tests for new behavior.

## License

This project is released under the MIT License — see the `LICENSE` file for details.
