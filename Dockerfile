FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY internal/ internal/
COPY docs/ docs/
RUN go build -o /sftpgo-manager .

FROM alpine:3.20
COPY --from=build /sftpgo-manager /usr/local/bin/sftpgo-manager
ENTRYPOINT ["sftpgo-manager"]
