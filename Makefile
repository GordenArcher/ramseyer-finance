APP_NAME=Ramseyer Finance
BINARY_NAME=ramseyer-finance

.PHONY: run test build macos-app windows-bundle clean

run:
	go run main.go

test:
	go test ./...

build:
	mkdir -p build
	go build -o build/$(BINARY_NAME) main.go

macos-app:
	./scripts/build_macos_app.sh

windows-bundle:
	@printf '%s\n' 'Run this target on Windows PowerShell:'
	@printf '%s\n' '.\scripts\build_windows_bundle.ps1'

clean:
	rm -rf build dist
