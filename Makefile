.PHONY: mocks test

mocks:
	@echo "Generating mocks..."
	@mkdir -p mocks
	@PATH="$(PATH):$(shell go env GOPATH)/bin" mockgen -source=goutbox.go -destination=mocks/mock_store.go -package=mocks
	@echo "Mocks generated successfully"

test:
	go test -v -race ./...
