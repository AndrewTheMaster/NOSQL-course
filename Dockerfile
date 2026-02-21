FROM golang:1.25.1 as builder

WORKDIR /app
COPY . .


RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/app ./cmd/app

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/bin/app /app/bin/app

ENV APP_PORT=${APP_PORT}

EXPOSE ${APP_PORT}
CMD ["/app/bin/app"]
