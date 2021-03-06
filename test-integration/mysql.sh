#!/bin/sh

OPTIONS="-config=test-integration/dbconfig.yml -env mysql"

set -ex

sql-migrate status $OPTIONS
sql-migrate up $OPTIONS
sql-migrate down $OPTIONS
sql-migrate redo $OPTIONS
sql-migrate status $OPTIONS
