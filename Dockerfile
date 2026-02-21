FROM golang:1.21 as builder

WORKDIR /app
COPY . .

RUN go mod init myapp && go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/app ./cmd/app

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/bin/app /app/bin/app

ENV APP_PORT=${APP_PORT}

EXPOSE ${APP_PORT}
CMD ["/app/bin/app"]
