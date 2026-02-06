FROM golang:1.25.7-alpine AS build
WORKDIR /src

COPY go.mod ./
COPY go.sum ./
COPY cmd ./cmd
COPY internal ./internal

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /out/enviro-ingest ./cmd/server

FROM alpine:3.22
RUN adduser -D -H -u 10001 appuser
WORKDIR /app

COPY --from=build /out/enviro-ingest /usr/local/bin/enviro-ingest
USER appuser

ENV PORT=8080
EXPOSE 8080

CMD ["enviro-ingest"]
