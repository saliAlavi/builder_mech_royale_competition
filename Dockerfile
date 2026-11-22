# Mech Royale bot (mechfight/1), written in Go. Build: docker build -t my-bot .
# The match runs it with no network, a read-only root (writable /tmp), 1 CPU, 512 MB, uid 65534, map at /arena.
FROM docker.io/library/golang:1.23.12-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/mechbot .

FROM scratch
WORKDIR /bot
COPY --from=build /out/mechbot /bot/mechbot
# fallback copy of the map (the match mounts the same map at /arena)
COPY kit/arena /bot/arena
USER 65534:65534
ENTRYPOINT ["/bot/mechbot"]
