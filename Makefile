GO ?= go
BIN := bin

.PHONY: all build server agent adopt test vet fmt clean

all: build

build: server agent adopt

adopt:
	CGO_ENABLED=0 $(GO) build -ldflags "-s -w" -o $(BIN)/logos-adopt ./agent/cmd/logos-adopt

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
