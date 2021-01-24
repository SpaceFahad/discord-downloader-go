FROM golang:1.15 AS builder

COPY . /go/src/github.com/github.com/get-got/discord-downloader-go
WORKDIR /go/src/github.com/github.com/get-got/discord-downloader-go

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o app .

FROM scratch
WORKDIR /root/
COPY --from=builder /go/src/github.com/github.com/get-got/discord-downloader-go/app .

ENTRYPOINT ["./app"]