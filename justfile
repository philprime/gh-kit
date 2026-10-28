binary := if os() == "windows" { "gh-kit.exe" } else { "gh-kit" }

build:
    go build -o {{binary}} .

test:
    go test ./...

test-race:
    go test -race ./...

vet:
    go vet ./...

tidy:
    go mod tidy

install:
    go mod download
    go install tool

analyze: vet
    go tool staticcheck ./...
    go tool govulncheck ./...

format:
    gofmt -w .
    dprint fmt
    go tool actionlint

format-check:
    gofmt -d .
    test -z "$(gofmt -l .)"
    dprint check
    go tool actionlint

run: build
    ./{{binary}}

install-extension: build
    gh extension install .
