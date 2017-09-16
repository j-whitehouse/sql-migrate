#!/bin/sh -e

# Add go binaries to path
export PATH=$PATH:/go/bin

# Install and run unit tests
go get -t
go test -coverprofile=/tmp/go-code-cover -timeout 30s

# Run CI tests
test-integration/postgres.sh
test-integration/mysql.sh
test-integration/mysql-flag.sh
test-integration/sqlite.sh

echo "CI Tests Ran Successfully"