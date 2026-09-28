# Build stage: compile a static binary.
FROM golang:1.24-alpine AS build
WORKDIR /src

# Download dependencies first so this layer is cached between builds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/api ./cmd/api

# Run stage: only the binary, no shell, runs as a non-root user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
