VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/coredipper/claude-seal/cmd.Version=$(VERSION)"

.PHONY: build install test lint clean

build:
	go build $(LDFLAGS) -o claude-seal .

install:
	go install $(LDFLAGS) .

test:
	go test ./... -count=1

test-verbose:
	go test ./... -v -count=1

lint:
	golangci-lint run

clean:
	rm -f claude-seal
