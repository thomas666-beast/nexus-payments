# --- build stage ---
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Cache deps first
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/nexus ./cmd/nexus

# --- runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/nexus /nexus

USER nonroot:nonroot
EXPOSE 8080 9090

ENTRYPOINT ["/nexus"]
