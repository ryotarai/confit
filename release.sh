#!/bin/sh
if [ -z "$1" ]; then
    echo "Usage: release.sh VERSION"
    exit 1
fi

set -ex

#gox -os="darwin linux windows" -arch="386 amd64" -output="pkg/{{.Dir}}_{{.OS}}_{{.Arch}}"
gox -os="linux" -arch="386 amd64" -output="pkg/{{.Dir}}_{{.OS}}_{{.Arch}}"
git tag v$1
git push --tags
ghr -u ryotarai v$1 pkg/
