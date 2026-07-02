# Stage 1: Build the React Frontend
FROM node:18-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm install
COPY frontend .
RUN npm run build

# Stage 2: Build the Go Backend
FROM golang:1.25-alpine AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN go build -o lb main.go admin.go

# Stage 3: Final Production Image
FROM alpine:latest
WORKDIR /app
# Copy the compiled Go binary
COPY --from=backend-builder /app/lb .
# Copy the built React assets into the expected directory
COPY --from=frontend-builder /app/frontend/dist ./frontend/dist
# Expose the ports (Documentation purposes)
EXPOSE 8080

# Run the binary
CMD ["./lb"]
