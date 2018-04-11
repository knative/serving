FROM golang

RUN mkdir -p $GOPATH/src/github.com/josephburnett/k8sflag
ADD . $GOPATH/src/github.com/josephburnett/k8sflag
RUN go get -u github.com/fsnotify/fsnotify

RUN cd $GOPATH/src/github.com/josephburnett/k8sflag/example && go build -o /hello hello.go

EXPOSE 8080

ENTRYPOINT ["/hello"]
