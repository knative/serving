FROM golang:latest

ENV APP ${GOPATH}/src/github.com/elafros/elafros/test/e2e/test_images/helloworld-grpc

RUN mkdir -p "$APP"
ADD . "$APP"
RUN go get google.golang.org/grpc && \
    go get golang.org/x/net/context

WORKDIR "$APP"
RUN go build -o /app/helloworld-grpc .
CMD ["/app/helloworld-grpc"]
