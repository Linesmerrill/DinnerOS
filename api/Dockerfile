# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
# The push sweep Heroku Scheduler runs as `/sendreminders` (docs/deployment.md).
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sendreminders ./cmd/sendreminders
# The starter recipe backfill, run by hand as `heroku run /seedstarter ...`.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/seedstarter ./cmd/seedstarter
# The first-day-of-the-week backfill, run by hand as `heroku run /weekstart ...`.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/weekstart ./cmd/weekstart
# The meal-kit recipe import worker Heroku Scheduler runs as `/importmealkit`
# (docs/meal-kit-import.md).
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/importmealkit ./cmd/importmealkit

# Heroku starts containers with `/bin/sh -c <CMD>`, so the runtime image needs a
# shell (distroless has none). Keep it minimal and non-root.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 app
COPY --from=build /out/server /server
COPY --from=build /out/sendreminders /sendreminders
COPY --from=build /out/seedstarter /seedstarter
COPY --from=build /out/weekstart /weekstart
COPY --from=build /out/importmealkit /importmealkit
EXPOSE 8080
USER app
# Heroku injects PORT; the server reads it from the environment.
CMD ["/server"]
