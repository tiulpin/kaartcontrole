BINARY_NAME=kc
VERSION=0.1.0
GOARCH=amd64

.PHONY: build install clean

build:
	go build -o bin/$(BINARY_NAME) ./cmd

install: build
	mkdir -p bin

clean:
	rm -rf bin/
