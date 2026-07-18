# syntax=docker/dockerfile:1

# ---- build stage: compile a static, stripped binary (cross-compiled for the target) ----
FROM --platform=$BUILDPLATFORM golang:1.26.3-alpine AS build

WORKDIR /src

# Download modules in their own layer so source changes don't bust the module cache.
COPY golang/go.mod golang/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY golang/ ./

# CGO off => fully static binary; -trimpath + -s -w for reproducible, smaller output.
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/tyk-sre-assignment .

# ---- runtime stage: distroless static, non-root, CA certs included, no shell ----
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.source="https://github.com/75asu/tyk-sre-assignment"

COPY --from=build /out/tyk-sre-assignment /tyk-sre-assignment

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/tyk-sre-assignment"]
