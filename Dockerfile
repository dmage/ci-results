FROM golang:alpine AS builder
RUN apk add build-base
WORKDIR /app
ADD . .
RUN go build .

FROM alpine
WORKDIR /app
COPY --from=builder /app/ci-results .
EXPOSE 8001
CMD /app/ci-results indexer -v=2 && /app/ci-results server -v=2
