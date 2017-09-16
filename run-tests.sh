#!/bin/sh

go test -v ./...
test-integration/postgres.sh
test-integration/mysql.sh
test-integration/mysql-flag.sh
test-integration/mysql-env.sh
test-integration/sqlite.sh
