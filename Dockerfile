FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /tchat .

FROM gcr.io/distroless/static
COPY --from=build /tchat /tchat
EXPOSE 8080
ENTRYPOINT ["/tchat", "serve"]
