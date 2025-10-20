FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum* ./
RUN if [ -f go.mod ]; then go mod download; fi

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o recommendations-service .

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/recommendations-service .

# This environment variable controls failure simulation
ENV SIMULATE_FAILURE=false

EXPOSE 8082

CMD ["./recommendations-service"]