# Build the control plane from the repo root:
#   docker build -f deploy/Dockerfile -t logos-server .
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.work ./
COPY server/go.mod server/go.sum ./server/
COPY agent/go.mod ./agent/
RUN cd server && go mod download
COPY server ./server
COPY agent ./agent
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /logos-server ./server/cmd/logos-server

FROM alpine:3.21
RUN adduser -D -H logos
COPY --from=build /logos-server /usr/local/bin/logos-server
USER logos
EXPOSE 8080
ENTRYPOINT ["logos-server"]
