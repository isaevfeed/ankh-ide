# Ankh — build on macOS (Apple Silicon or Intel).
# Needs: Go 1.25+, Node 20+, Xcode Command Line Tools (xcode-select --install).

export MACOSX_DEPLOYMENT_TARGET := 11.0
export CGO_ENABLED := 1

.PHONY: setup frontend build run app universal install test dev-web clean

setup:            ## download Go and npm dependencies (run once)
	go mod download
	cd frontend && npm ci

frontend:         ## build the UI into frontend/dist (embedded into the binary)
	cd frontend && npm run build

build:            ## build bin/ankh (uses the UI already in frontend/dist)
	go build -o bin/ankh .

run: app          ## build Ankh.app and open it
	open build/bin/Ankh.app

app: frontend     ## build build/bin/Ankh.app
	./scripts/package-macos.sh

universal: frontend ## build Ankh.app for Apple Silicon and Intel
	ARCH=universal ./scripts/package-macos.sh

install: app      ## copy Ankh.app to /Applications
	rm -rf /Applications/Ankh.app
	cp -R build/bin/Ankh.app /Applications/
	touch /Applications/Ankh.app
	/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f /Applications/Ankh.app || true
	killall Dock || true   # the Dock caches icons of apps that are not running

test:             ## Go and frontend tests
	go vet ./internal/... ./cmd/...
	go test ./internal/...
	cd frontend && npm run typecheck && npm test

dev-web:          ## UI in a browser at http://127.0.0.1:7777 (no Wails)
	cd frontend && npm run build:http
	go run ./cmd/devserver

clean:
	rm -rf bin build/bin frontend/dist
