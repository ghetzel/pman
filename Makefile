.PHONY: fmt deps build
.EXPORT_ALL_VARIABLES:

GO111MODULE     ?= on
LOCALS          := $(shell find . -type f -name '*.go' 2> /dev/null)

all: deps fmt build

fmt:
	# go generate -x
	gofmt -w $(LOCALS)
	go vet ./...

deps:
	go get ./...
	-go mod tidy

build:
	go build -o bin/pman *.go
	which pman && cp -v bin/pman `which pman` || true