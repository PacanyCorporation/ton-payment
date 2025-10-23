VERSION := latest
GIT_TAG := $(shell git describe --tags --always)

build:
	@echo "Building tag: $(GIT_TAG)"
	docker build --build-arg GIT_TAG=$(GIT_TAG) -t payment-processor:$(VERSION) --target payment-processor .
	docker build --build-arg GIT_TAG=$(GIT_TAG) -t payment-test:$(VERSION) --target payment-test .

wallet-tool:
	@echo "Building wallet-tool..."
	go build -o bin/wallet-tool ./cmd/wallet-tool
	@echo "✓ Wallet tool built successfully: bin/wallet-tool"

wallet-tool-install:
	@echo "Installing wallet-tool..."
	go install ./cmd/wallet-tool
	@echo "✓ Wallet tool installed to GOPATH/bin"