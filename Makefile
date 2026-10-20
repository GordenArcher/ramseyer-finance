APP_NAME=Ramseyer Finance
BINARY_NAME=ramseyer-finance

.PHONY: run test build clean

run:
	go run main.go

test:
	go test ./...

build:
	mkdir -p build
	go build -o build/$(BINARY_NAME) main.go

clean:
	rm -rf build dist
