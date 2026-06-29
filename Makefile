.PHONY: build test run clean

BINARY := ban-bot

build:
	go build -o $(BINARY) .

test:
	go test -v ./...

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
