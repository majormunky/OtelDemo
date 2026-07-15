.PHONY: proto run build clean jaeger-up jaeger-down

# Regenerate Go code from the proto definitions
proto:
	@protoc -I . -I third_party/googleapis \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		--grpc-gateway_out=. --grpc-gateway_opt=paths=source_relative \
		proto/echo/echo.proto && echo 'Proto Generation Completed..'

# Run the server locally
run:
	go run .

# Build a binary
build:
	go build -o bin/echo-server .

# Remove build artifacts
clean:
	rm -rf bin/

# Start the local Jaeger all-in-one container
jaeger-up:
	podman run -d --name jaeger \
		-p 16686:16686 \
		-p 4317:4317 \
		-p 4318:4318 \
		docker.io/jaegertracing/all-in-one:latest

# Stop and remove the Jaeger container
jaeger-down:
	podman stop jaeger
	podman rm jaeger
