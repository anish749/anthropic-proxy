.PHONY: build run

build:
	go build -o anthropic-proxy .

run: build
	./anthropic-proxy $(ARGS)
