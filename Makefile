VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
export CGO_ENABLED := 0

.PHONY: all linux windows test clean

all: linux windows

linux:
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/powerhook-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/powerhook-linux-arm64 .

windows:
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/powerhook-windows-amd64.exe .

test:
	go vet ./... && GOOS=windows go vet ./... && go test ./...

clean:
	rm -rf dist
