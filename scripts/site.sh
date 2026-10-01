#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
version=$(cat .hugo-version)
hugo=$PWD/bin/tools/hugo
if [ ! -x "$hugo" ]; then
  echo 'Run make tools-site to install the pinned Hugo compiler.' >&2
  exit 1
fi
case "$("$hugo" version)" in "hugo v$version "*) ;; *) echo "Hugo must match .hugo-version ($version); run make tools-site" >&2; exit 1 ;; esac
case "${1:-build}" in
  build) "$hugo" --source site --destination ../bin/site --minify --panicOnWarning ;;
  serve) "$hugo" server --source site --bind 127.0.0.1 --baseURL http://localhost:1313/ --disableFastRender ;;
  *) echo 'Use build or serve' >&2; exit 1 ;;
esac
