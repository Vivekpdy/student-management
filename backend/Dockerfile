
# Use latest Go image
FROM golang:alpine

# Working directory
WORKDIR /app

# Copy all backend files
COPY . .

# Download Go dependencies
RUN go mod download

# Build Go application
RUN go build -o server .

# Application port
EXPOSE 8081

# Start application
CMD ["./server"]
