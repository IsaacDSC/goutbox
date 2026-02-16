.PHONY: mocks test test-all

mocks:
	@echo "Generating mocks..."
	@mkdir -p mocks
	@PATH="$(PATH):$(shell go env GOPATH)/bin" mockgen -source=goutbox.go -destination=mocks/mock_store.go -package=mocks
	@echo "Mocks generated successfully"

test:
	go test -v -race ./...

test-all:
	@echo "Testing core module..."
	go test -v -race ./...
	@echo "Testing stores/postgres module..."
	cd stores/postgres && go test -v -race ./...
