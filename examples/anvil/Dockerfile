# syntax=docker/dockerfile:1
# One Dockerfile for all three components: docker build --build-arg APP=api|worker|web .

FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine AS build
ARG APP
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    test -n "$APP" && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X bluepave.dev/examples/anvil/internal/server.Version=$VERSION" \
      -o /out/app ./cmd/$APP

# Distroless static: no shell, no package manager, runs as uid 65532. Only the binary ships.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app"]
