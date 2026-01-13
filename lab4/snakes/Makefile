lint: protolint golint

protobuf:
	protoc --go_out=. --go_opt=paths=source_relative \
				 proto/snakes.proto

protolint:
	protolint .

golint:
	golangci-lint run -E gocritic -v ./...
