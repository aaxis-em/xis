# Contributing

## Build and test

1. Install Go 1.26 or later.
2. Run `make build` to build `bin/xis`.
3. Run `make test` to run the tests.
4. Run `make lint` to run `go vet` and `golangci-lint`.
5. Run `make docs-lint` to check the documents.

## Documents

Write the documents in Simplified Technical English (ASD-STE100). Use short sentences, active voice and one word for one meaning. Use the terms in [docs/GLOSSARY.md](docs/GLOSSARY.md). Run `make docs-lint` before you commit.
