# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
# The push sweep Heroku Scheduler runs as `/sendreminders` (docs/deployment.md).
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sendreminders ./cmd/sendreminders

# Heroku starts containers with `/bin/sh -c <CMD>`, so the runtime image needs a
# shell (distroless has none). Keep it minimal and non-root.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 app
COPY --from=build /out/server /server
COPY --from=build /out/sendreminders /sendreminders
EXPOSE 8080
USER app
# Heroku injects PORT; the server reads it from the environment.
CMD ["/server"]
