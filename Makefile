.PHONY: build test run clean install

BINARY := ban-bot
SOURCES := $(wildcard *.go)

build: $(BINARY)

$(BINARY): $(SOURCES) go.mod go.sum
	go build -o $(BINARY) .

test:
	go test -v ./...

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)

install: build
	sudo ./install.sh
