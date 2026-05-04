GO ?= go
BIN := scoutbook-xls

.PHONY: build test test-verbose test-race lint tidy clean run

build:
	$(GO) build -o $(BIN) .

test:
	$(GO) test ./...

test-verbose:
	$(GO) test -v ./...

test-race:
	$(GO) test -race ./...

lint:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BIN) coverage.out coverage.html

run: build
	./$(BIN)
