# Inputs are exact covered/total statement counts, not rounded percentages.
function readratio(value, pair) {
  if (value !~ /^[0-9]+ [0-9]+$/) { print "Invalid coverage ratio"; exit 1 }
  split(value, pair, " ")
  if (pair[2] == 0 || pair[1] > pair[2]) { print "Invalid coverage counts"; exit 1 }
}
BEGIN {
  readratio(measured, m); readratio(baseline, b); readratio(previous, p)
  if (b[1]*p[2] < p[1]*b[2]) { print "Coverage baseline cannot decrease"; exit 1 }
  if (m[1]*b[2] < b[1]*m[2]) { print "Coverage decreased below baseline"; exit 1 }
  if (update != 1 && m[1]*b[2] > b[1]*m[2]) {
    print "Coverage improved: run make coverage-update and commit the baseline"
    exit 1
  }
  printf "Statement coverage: %.2f%%\n", 100*m[1]/m[2]
}
