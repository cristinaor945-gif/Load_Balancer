# Stage 1: Build React
FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend

COPY frontend/package*.json ./
RUN npm install

COPY frontend .
RUN npm run build

# Stage 2: Build Go
FROM golang:1.25-alpine AS backend-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o lb main.go admin.go
RUN go build -o backend1 Backend1.go
RUN go build -o backend2 Backend2.go

# Stage 3
FROM alpine:latest

WORKDIR /app

COPY --from=backend-builder /app/lb .
COPY --from=backend-builder /app/backend1 .
COPY --from=backend-builder /app/backend2 .

COPY config.json .

COPY --from=frontend-builder /app/frontend/dist ./frontend/dist

COPY start.sh .

RUN chmod +x start.sh

EXPOSE 8080

CMD ["./start.sh"]