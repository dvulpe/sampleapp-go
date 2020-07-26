FROM golang:1.14-alpine as builder

WORKDIR /go/src/sampleapp-go

COPY . .

RUN go build -o sampleapp

FROM alpine:latest

RUN apk --no-cache add ca-certificates

COPY --from=builder /go/src/sampleapp-go/sampleapp /usr/local/bin/sampleapp

ENTRYPOINT ["/usr/local/bin/sampleapp"]
