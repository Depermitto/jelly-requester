FROM golang:1.24-alpine AS build-stage
WORKDIR /build
COPY . .
RUN go mod download
RUN go build -o main
RUN apk add binutils
RUN strip main

FROM gcr.io/distroless/static AS production-stage
WORKDIR /app
COPY --from=build-stage /build/main .
ENTRYPOINT ["./main"]
