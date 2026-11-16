#!/bin/bash
# Measures Headroom's own CPU and memory use (README "Performance").
#
#   scripts/measure.sh [path/to/Headroom.app] [minutes]
#
# Launches the app, leaves its panels closed, and samples every 10 s:
# the main process and the WebKit helper processes that start with it
# (they are not its children, so they are found by comparing the list
# before and after launch). Quits the app normally at the end.
set -euo pipefail
APP="${1:-build/out/Headroom.app}"
APP="$(cd "$(dirname "$APP")" && pwd)/$(basename "$APP")"
MINUTES="${2:-10}"
BIN="$APP/Contents/MacOS/Headroom"

webkit_pids() { pgrep -f "com.apple.WebKit" | sort || true; }

before="$(webkit_pids)"
open -n -g "$APP"
pid=""
for _ in $(seq 1 50); do
  pid="$(pgrep -fx "$BIN" | head -1 || true)"
  [ -n "$pid" ] && break
  sleep 0.2
done
[ -n "$pid" ] || { echo "Headroom did not start"; exit 1; }
sleep 8
helpers="$(comm -13 <(echo "$before") <(webkit_pids) | tr '\n' ' ')"

cpu_seconds() { # cumulative CPU time of the given pids, in seconds
  ps -o time= -p "$1" 2>/dev/null | awk -F'[:.]' '{ if (NF==3) s+=$1*60+$2+$3/100; else s+=$1*3600+$2*60+$3+$4/100 } END { printf "%.2f", s }'
}
rss_mb() { ps -o rss= -p "$1" 2>/dev/null | awk '{ s+=$1 } END { printf "%.1f", s/1024 }'; }
fp_mb() { # physical footprint (what Activity Monitor calls Memory)
  /usr/bin/footprint -p "$1" 2>/dev/null | awk '/phys_footprint:/ { v=$2; u=$3; if (u ~ /^G/) v*=1024; if (u ~ /^K/) v/=1024; print v; exit }'
}

hl=$(echo $helpers | tr ' ' ',')
c0=$(cpu_seconds "$pid"); h0=$([ -n "$hl" ] && cpu_seconds "$hl" || echo 0)
t0=$(date +%s)
echo "time_s,main_rss_mb,main_footprint_mb,helpers_rss_mb"
end=$((t0 + MINUTES * 60))
peak=0
while [ "$(date +%s)" -lt "$end" ]; do
  m=$(rss_mb "$pid"); f=$(fp_mb "$pid"); hr=$([ -n "$hl" ] && rss_mb "$hl" || echo 0)
  echo "$(( $(date +%s) - t0 )),$m,${f:-?},$hr"
  peak=$(awk -v a="$peak" -v b="${f:-0}" 'BEGIN { print (b>a)?b:a }')
  sleep 10
done
t1=$(date +%s)
c1=$(cpu_seconds "$pid"); h1=$([ -n "$hl" ] && cpu_seconds "$hl" || echo 0)
el=$((t1 - t0))
echo
echo "Minutes measured:            $MINUTES"
echo "Main process CPU (average):  $(awk -v a="$c0" -v b="$c1" -v e="$el" 'BEGIN { printf "%.2f%% of one core", (b-a)/e*100 }')"
echo "Web views CPU (average):     $(awk -v a="$h0" -v b="$h1" -v e="$el" 'BEGIN { printf "%.2f%% of one core", (b-a)/e*100 }')"
echo "Main process memory (peak):  ${peak} MB physical footprint"
echo "Web view processes:          $(echo $helpers | wc -w | tr -d ' ') ($(rss_mb "${hl:-0}") MB resident)"
kill -TERM "$pid" # a normal quit request, never kill -9
