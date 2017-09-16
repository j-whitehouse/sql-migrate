FROM golang:1.9-alpine

WORKDIR /go/src/sql-migrate
COPY . .

RUN apk add --no-cache git gcc musl-dev
RUN go get -d -v ./...
RUN go install -v ./sql-migrate

CMD ["/go/bin/sql-migrate"]
