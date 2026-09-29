#!/bin/sh
set -eu
assert_gate() {
  expected=$1
  shift
  actual=0
  awk -f scripts/coverage.awk "$@" >/dev/null || actual=$?
  if [ "$actual" != "$expected" ]; then
    echo "Coverage gate test failed: expected $expected, got $actual ($*)" >&2
    exit 1
  fi
}
assert_gate 0 -v measured='8 10' -v baseline='4 5' -v previous='3 4'
assert_gate 1 -v measured='7 10' -v baseline='4 5' -v previous='4 5'
assert_gate 1 -v measured='9 10' -v baseline='4 5' -v previous='9 10'
assert_gate 1 -v measured='9 10' -v baseline='4 5' -v previous='4 5'
assert_gate 0 -v measured='9 10' -v baseline='4 5' -v previous='4 5' -v update=1
assert_gate 1 -v measured='79999 100000' -v baseline='4 5' -v previous='4 5'
assert_gate 1 -v measured='9 10' -v baseline='garbage' -v previous='4 5'
assert_gate 1 -v measured='0 0' -v baseline='4 5' -v previous='4 5'
assert_gate 1 -v measured='11 10' -v baseline='4 5' -v previous='4 5'
