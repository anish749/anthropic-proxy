.PHONY: build dev

build:
	go build -o anthropic-proxy .

dev: build
	./anthropic-proxy $(ARGS)
