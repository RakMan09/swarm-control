# Multi-stage build for all Go backend services. Each compose service selects
# its binary via the `command:` field.
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/ingestion ./ingestion && \
    CGO_ENABLED=0 GOOS=linux go build -o /out/alerts     ./alerts && \
    CGO_ENABLED=0 GOOS=linux go build -o /out/command     ./command && \
    CGO_ENABLED=0 GOOS=linux go build -o /out/api         ./api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /app/
WORKDIR /app
# Default command; overridden per service in docker-compose.
CMD ["/app/api"]
