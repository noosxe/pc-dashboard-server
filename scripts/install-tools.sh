#!/bin/bash

set -e

echo "Installing Go-based development tools..."

# sqlc for database code generation
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

# goose for database migrations
go install github.com/pressly/goose/v3/cmd/goose@latest

# Protobuf and ConnectRPC generation
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install connectrpc.com/connect/cmd/protoc-gen-connect-go@latest

# Buf for protobuf management
go install github.com/bufbuild/buf/cmd/buf@latest

# Air for hot-reloading
go install github.com/air-verse/air@latest

# golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# beads (bd) for issue tracking
CGO_ENABLED=0 go install github.com/steveyegge/beads/cmd/bd@latest

echo "All tools installed successfully!"
