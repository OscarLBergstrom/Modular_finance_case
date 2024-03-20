# Use the official Go image as a parent image
FROM golang:alpine

# Set the working directory in the container
WORKDIR /app

# Copy the local code to the container's workspace
COPY . .

# Download Go modules
RUN go mod download

# Build the application
RUN go build -o websub-hub .

# Run the application
CMD ["./websub-hub"]
