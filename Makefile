GO ?= go
BIN := bin

.PHONY: all build server agent adopt imagebuilder test vet fmt clean

all: build

build: server agent adopt imagebuilder

adopt:
	CGO_ENABLED=0 $(GO) build -ldflags "-s -w" -o $(BIN)/logos-adopt ./agent/cmd/logos-adopt

imagebuilder:
	CGO_ENABLED=0 $(GO) build -ldflags "-s -w" -o $(BIN)/logos-imagebuilder ./agent/cmd/logos-imagebuilder

server:
	$(GO) build -o $(BIN)/logos-server ./server/cmd/logos-server

agent:
	CGO_ENABLED=0 $(GO) build -ldflags "-s -w" -o $(BIN)/logos-agent ./agent/cmd/logos-agent

test:
	$(GO) test ./server/... ./agent/...

vet:
	$(GO) vet ./server/... ./agent/...

fmt:
	gofmt -l -w server agent

clean:
	rm -rf $(BIN)
