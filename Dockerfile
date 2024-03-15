FROM golang:1.20 as build

WORKDIR /go/src/app
COPY . .

RUN go mod download
RUN CGO_ENABLED=0 go build -o /go/bin/filehole

FROM debian:12
COPY --from=build /go/bin/filehole /
CMD ["/filehole", "serve"]
