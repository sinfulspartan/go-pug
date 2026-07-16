module github.com/sinfulspartan/go-pug/benchmark/vs-joker

go 1.26

require github.com/sinfulspartan/go-pug v0.0.0-00010101000000-000000000000

require (
	github.com/Joker/jade v1.1.3 // indirect
	golang.org/x/mod v0.5.1 // indirect
	golang.org/x/sys v0.0.0-20211019181941-9d821ace8654 // indirect
	golang.org/x/tools v0.1.9 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
)

replace github.com/sinfulspartan/go-pug => ../..

tool github.com/Joker/jade/cmd/jade
