FROM golang
WORKDIR /app
ADD . .
RUN go build .
EXPOSE 8001
CMD /app/ci-results indexer -v=2 && /app/ci-results server -v=2
