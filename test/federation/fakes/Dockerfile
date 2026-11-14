# Built from the repository root: docker build -f test/federation/fakes/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY test/federation/fakes ./test/federation/fakes
RUN CGO_ENABLED=0 go build -o /out/fakes ./test/federation/fakes

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/fakes /fakes
EXPOSE 53/udp 53/tcp 8080
ENTRYPOINT ["/fakes"]
