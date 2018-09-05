FROM golang:1.11-alpine3.8 as builder

RUN apk add --no-cache git

ENV GO111MODULE=on
ENV PACKAGE github.com/mopsalarm/go-pr0gramm-categories
WORKDIR $GOPATH/src/$PACKAGE/

COPY go.mod go.sum ./
RUN go mod download

ENV CGO_ENABLED=0

COPY . .
RUN go build -o /go-pr0gramm-categories -v .


FROM alpine:3.8
RUN apk add --no-cache ca-certificates
EXPOSE 8080

COPY --from=builder /go-pr0gramm-categories /

ENTRYPOINT ["/go-pr0gramm-categories"]
