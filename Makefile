.PHONY: build build-all test vet clean

BINARY=plugin
PLATFORMS=linux/amd64 linux/arm64 darwin/arm64
VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//')
LDFLAGS=-s -w -X main.version=$(VERSION)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) .

test:
	CGO_ENABLED=0 go test ./...

vet:
	CGO_ENABLED=0 go vet ./...

build-all:
	@mkdir -p dist; for platform in $(PLATFORMS); do \
		GOOS=$${platform%%/*} GOARCH=$${platform##*/} CGO_ENABLED=0 \
		go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-$${platform%%/*}-$${platform##*/} .; \
	done

clean:
	rm -rf $(BINARY) dist
