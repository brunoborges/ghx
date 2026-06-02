all: build

build:
	go build -o bin/ghx ./src/cmd/ghx
	go build -o bin/ghxd ./src/cmd/ghxd

test:
	go test ./...

clean:
	rm -rf bin/

.PHONY: all build test clean
