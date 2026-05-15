FROM golang:1.26-alpine AS dev

WORKDIR /app

RUN go install github.com/air-verse/air@latest

COPY . .

EXPOSE 8080

CMD ["air", "-c", ".air.toml"]
