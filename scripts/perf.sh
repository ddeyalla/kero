#!/bin/sh
# perf.sh measures what Shibu Code costs with N panes, against the budgets
# in docs/perf.md:
#
#   sh scripts/perf.sh 4
#
# It builds this checkout, starts a kerod of its own (a temporary
# KERO_HOME, HOME and socket; every KERO_* variable of the caller cleared,
# so the daily app and its kerod are never reached), opens N panes and feeds
# each PERF_FEED_MB megabytes of output (16 by default, past the default
# scrollback of 4 MB, so that every pane's scrollback is full), and then:
#
#   1. waits PERF_SETTLE seconds (15), past kerod's 10 s history save;
#   2. samples kerod and the app over separate focused and visible/unfocused
#      PERF_IDLE second (60) windows, with native state checked independently;
#   3. measures 100 native key dispatches through PTY echo to paint, with
#      the paint-matching hook disabled during idle and frame timing;
#   4. has the pane the app shows stream output for
#      PERF_BUSY seconds (10), for the frame times of MYGO_FRAME_STATS;
#   5. prints the table, and stops everything it started.
#
# PERF_MEMORY=1 visits every terminal twice, streams all of them, then
# collects settled footprint, Go heaps and native VM accounting. It uses
# a temporary diagnostics build overlay; PERF_KEEP=1 preserves evidence.
# PERF_LAYOUT=split uses N panes in one session instead of N sessions.
# Memory runs include forced diagnostic GC and are not idle/CPU benchmarks.
# Normal app runs also measure 100 native AppKit key dispatches through
# the PTY echo to paint, with focus observed independently. PERF_GUI=0
# selects the older uninjected workload; PERF_APP=0 measures kerod alone.
#
# PERF_PPROF=127.0.0.1:6060 has that kerod serve its profiles there
# (KERO_PPROF, internal/daemon/pprof.go) while the run lasts.
#
# The app is this build's, packaged in its own uniquely identified temporary
# bundle and launched through LaunchServices, with HOME and TMPDIR of its own:
# its name and identity are not
# the daily app's, nor a dev app's, and its single-instance lock is in its
# own TMPDIR. It opens a window for the run. PERF_APP=0 leaves it out, and
# the table says so.
#
# PERF_IDLE and PERF_SETTLE shorter than the defaults are for a quick check
# of the script itself; the numbers in docs/perf.md use the defaults.
set -eu

panes=${1:-1}
case $panes in
'' | *[!0-9]*) echo "usage: sh scripts/perf.sh N   (N panes, 1 or more)" >&2; exit 2 ;;
esac
[ "$panes" -ge 1 ] || { echo "perf.sh: N must be 1 or more" >&2; exit 2; }
if [ "${PERF_MEMORY:-0}" = 0 ] && [ "${PERF_APP:-1}" != 0 ]; then
	: "${PERF_GUI:=1}"
else
	: "${PERF_GUI:=0}"
fi
export PERF_GUI
case ${PERF_MEMORY:-0} in 0|1) ;; *) echo "perf.sh: PERF_MEMORY must be 0 or 1" >&2; exit 2 ;; esac
case ${PERF_GUI:-0} in 0|1) ;; *) echo "perf.sh: PERF_GUI must be 0 or 1" >&2; exit 2 ;; esac
[ "${PERF_MEMORY:-0}${PERF_GUI:-0}" != 11 ] || { echo "perf.sh: choose PERF_MEMORY or PERF_GUI" >&2; exit 2; }
[ "${PERF_APP:-1}${PERF_GUI:-0}" != 01 ] || { echo "perf.sh: PERF_APP=0 conflicts with PERF_GUI=1" >&2; exit 2; }
case ${PERF_LAYOUT:-sessions} in
sessions) ;;
split) [ "${PERF_MEMORY:-0}" = 1 ] || { echo "perf.sh: PERF_LAYOUT=split requires PERF_MEMORY=1" >&2; exit 2; } ;;
*) echo "perf.sh: PERF_LAYOUT must be sessions or split" >&2; exit 2 ;;
esac
feed_mb=${PERF_FEED_MB:-16}
settle=${PERF_SETTLE:-15}
idle=${PERF_IDLE:-60}
busy=${PERF_BUSY:-10}

root=$(git rev-parse --show-toplevel)
cd "$root"
. scripts/dev-env.sh
python3 -I scripts/perf_memory.py validate
probe="python3 -I $root/scripts/perf_probe.py"

# Nothing of the caller's reaches what this starts.
unset KERO_HOME KERO_CONFIG KERO_SOCKET KERO_PANE KERO_SESSION KERO_PPROF \
	LINEAR_API_KEY XDG_RUNTIME_DIR MYGO_FRAME_STATS MYGO_DEV MYGO_ENV MYGO_GHOSTTY_VT

tmp=$(mktemp -d /tmp/kero-perf.XXXXXX)
tmp=$(cd "$tmp" && pwd -P)
bin="$tmp/shibu-perf"
if [ "${PERF_APP:-1}" != 0 ]; then
	bundle="$tmp/Shibu Performance.app"
	bin="$bundle/Contents/MacOS/shibu-perf"
fi
kerod_pid=
app_pid=
cleanup() {
	set +e
	if [ -n "$kerod_pid" ] && kill -0 "$kerod_pid" 2>/dev/null; then
		for id in $(cat "$tmp/sessions" 2>/dev/null); do
			"$bin" close "$id" >/dev/null 2>&1
		done
	fi
	[ -n "$app_pid" ] && kill "$app_pid" 2>/dev/null
	[ -n "$kerod_pid" ] && kill -TERM "$kerod_pid" 2>/dev/null
	for _ in 1 2 3 4 5 6 7 8 9 10; do
		{ [ -z "$kerod_pid" ] || ! kill -0 "$kerod_pid" 2>/dev/null; } &&
			{ [ -z "$app_pid" ] || ! kill -0 "$app_pid" 2>/dev/null; } && break
		sleep 0.5
	done
	[ -n "$app_pid" ] && kill -KILL "$app_pid" 2>/dev/null
	[ -n "$kerod_pid" ] && kill -KILL "$kerod_pid" 2>/dev/null
	left=$(pgrep -f "$tmp" 2>/dev/null)
	if [ -n "$left" ]; then
		echo "perf.sh: still running from $tmp: $left" >&2
	elif [ "${PERF_KEEP:-0}" = 1 ]; then
		echo "perf.sh: kept $tmp" >&2
	else
		rm -rf "$tmp"
	fi
}
trap cleanup EXIT
trap 'exit 130' INT TERM

if [ "${PERF_APP:-1}" != 0 ]; then
	# Only an app run needs a native bundle or its terminal library.
	mkdir -p "$bundle/Contents/MacOS"
	cat >"$bundle/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>shibu-perf</string>
<key>CFBundleIdentifier</key><string>dev.ddeyalla.shibu-perf.$(basename "$tmp")</string>
<key>CFBundleName</key><string>Shibu Performance</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>NSHighResolutionCapable</key><true/>
<key>NSPrincipalClass</key><string>NSApplication</string>
</dict></plist>
EOF
	ln -s "$bin" "$tmp/shibu-perf"
	mygo_module=$(go list -m -f '{{.Dir}}' github.com/egoist/mygo)
	python3 -I scripts/perf_memory.py bundle-native \
		"$mygo_module/plugins/terminal/mygo-plugin.json" "$bundle/Contents/Resources"
fi

echo "perf.sh: building $(git rev-parse --short HEAD)$(git diff --quiet HEAD -- . ':!docs' || echo '+changes')" >&2
if [ "${PERF_MEMORY:-0}" = 1 ] || [ "${PERF_GUI:-0}" = 1 ]; then
	if [ "${PERF_GUI:-0}" = 1 ]; then
		overlay=$(python3 -I scripts/perf_memory.py prepare "$root" "$tmp/diagnostics" --gui)
	else
		overlay=$(python3 -I scripts/perf_memory.py prepare "$root" "$tmp/diagnostics")
	fi
	CGO_ENABLED=0 go build -overlay "$overlay" -o "$bin" .
else
	CGO_ENABLED=0 go build -o "$bin" .
fi

mkdir -p "$tmp/home"
: >"$tmp/app.log"
export HOME="$tmp/home" KERO_HOME="$tmp/kero" KERO_SOCKET="$tmp/kerod.sock" KERO_CONFIG="$tmp/config"
printf 'command = /bin/sh\n' >"$KERO_CONFIG"

# The output fed to each pane: numbered, colored lines, as a build log.
awk -v bytes=$((feed_mb << 20)) 'BEGIN {
	for (i = 0; n < bytes; i++) {
		line = sprintf("\033[32m%7d\033[0m  ok   kero/internal/pkg%03d  %.3fs  the quick brown fox jumps over the lazy dog", i, i % 997, (i % 89) / 100)
		print line; n += length(line) + 1
	}
}' >"$tmp/feed.txt"
fed=$(wc -c <"$tmp/feed.txt" | tr -d ' ')

KERO_PPROF=${PERF_PPROF:-} "$bin" __kerod >>"$tmp/kerod.out" 2>&1 &
kerod_pid=$!
for _ in $(seq 100); do
	[ -S "$KERO_SOCKET" ] && break
	sleep 0.1
done
[ -S "$KERO_SOCKET" ] || { echo "perf.sh: kerod did not start (see $tmp/kerod.out)" >&2; exit 1; }
# kerod alone, before it has a pane: it exits after 30 s with none, so now.
sleep 1
$probe sample "$kerod_pid" >"$tmp/base.json"

app_reason=
start_app() {
	if [ "${PERF_APP:-1}" = 0 ]; then
		app_reason="PERF_APP=0"
	else
		mkdir -p "$tmp/apptmp"
		if [ "${PERF_MEMORY:-0}" = 1 ] || [ "${PERF_GUI:-0}" = 1 ]; then
			# LaunchServices grants this uniquely identified test bundle its
			# normal activation lifecycle. It does not inherit routing from
			# this shell, so supply every app/daemon identity explicitly.
			/usr/bin/open -n -a "$bundle" --stdout "$tmp/app.log" --stderr "$tmp/app.log" \
				--env "HOME=$HOME" --env "KERO_HOME=$KERO_HOME" \
				--env "KERO_CONFIG=$KERO_CONFIG" --env "KERO_SOCKET=$KERO_SOCKET" \
				--env KERO_PANE --env KERO_SESSION --env XDG_RUNTIME_DIR --env LINEAR_API_KEY \
				--env MYGO_ENV --env MYGO_DEV --env MYGO_GHOSTTY_VT --env MYGO_FRAME_STATS=all \
				--env "TMPDIR=$tmp/apptmp/" --env "SHIBU_PERF_DIAGNOSTICS=$tmp/diagnostics" \
				--env "GOMEMLIMIT=${GOMEMLIMIT:-}" --env "GOGC=${GOGC:-}" --env "GODEBUG=${GODEBUG:-}"
			for _ in $(seq 300); do
				[ -s "$tmp/diagnostics/app-pid" ] && break
				sleep 0.1
			done
			app_pid=$(cat "$tmp/diagnostics/app-pid" 2>/dev/null || true)
			case $app_pid in ''|*[!0-9]*) echo "perf.sh: app did not publish its PID" >&2; exit 1 ;; esac
			case $(ps -p "$app_pid" -o command=) in
				"$bin"*) ;;
				*) echo "perf.sh: diagnostic PID is not this test executable" >&2; app_pid=; exit 1 ;;
			esac
		else
			TMPDIR="$tmp/apptmp/" MYGO_FRAME_STATS=all "$bin" </dev/null >"$tmp/app.log" 2>&1 &
			app_pid=$!
			sleep 5
		fi
		if ! kill -0 "$app_pid" 2>/dev/null; then
			app_reason="it exited at start: $(tail -n 1 "$tmp/app.log" 2>/dev/null)"
			app_pid=
		fi
	fi
}

# Capture the same app before it has attached any terminal. This baseline
# is separate from the first-shown stage, which already contains replay.
if [ "${PERF_MEMORY:-0}" = 1 ]; then
	: >"$tmp/sessions"
	: >"$tmp/targets"
	start_app
	[ -n "$app_pid" ] || { echo "perf.sh: memory workload requires app" >&2; exit 1; }
	TMPDIR="$tmp/apptmp/" python3 -I scripts/perf_memory.py baseline "$tmp" "$app_pid" "$kerod_pid" "$panes" "$busy"
fi

# Each pane prints the feed, then waits for a line, which only the first
# is sent: then it streams output for PERF_BUSY seconds.
pane_program="cat '$tmp/feed.txt'; echo PERF-READY; read go
end=\$((\$(date +%s) + $busy)); while [ \$(date +%s) -lt \$end ]; do head -c 65536 '$tmp/feed.txt'; done
echo PERF-STREAMED; sleep 86400"
: >"$tmp/sessions"
: >"$tmp/targets"
split_batch=1
split_pos=1
direction=--right
for i in $(seq "$panes"); do
	if [ "${PERF_LAYOUT:-sessions}" = split ] && [ "$i" -gt 1 ]; then
		target=$(sed -n "${split_pos}p" "$tmp/targets")
		"$bin" split "$target" "$direction" -d "$tmp" -- /bin/sh -c "$pane_program" >>"$tmp/targets"
		# Split each leaf once before alternating axes for the next level.
		split_pos=$((split_pos + 1))
		if [ "$split_pos" -gt "$split_batch" ]; then
			split_batch=$((split_batch * 2))
			split_pos=1
			if [ "$direction" = --right ]; then direction=--down; else direction=--right; fi
		fi
	else
		id=$("$bin" new -n "perf-$i" -d "$tmp" -- /bin/sh -c "$pane_program")
		printf '%s\n' "$id" >>"$tmp/sessions"
		if [ "${PERF_LAYOUT:-sessions}" = split ]; then
			# Session targets follow the focused pane; save this pane's
			# own ID so later splits cannot move the fixture's target.
			"$bin" ls --json | python3 -I -c 'import json,sys; print(next(s["focus"] for s in json.load(sys.stdin) if s["id"] == sys.argv[1]))' "$id" >>"$tmp/targets"
		else
			printf '%s\n' "$id" >>"$tmp/targets"
		fi
	fi
done
echo "perf.sh: $panes pane(s), $((fed >> 20)) MB each; waiting for them to take it" >&2
for id in $(cat "$tmp/targets"); do
	for _ in $(seq 600); do
		"$bin" read "$id" 2>/dev/null | grep -q PERF-READY && break
		sleep 0.2
	done
	"$bin" read "$id" 2>/dev/null | grep -q PERF-READY || { echo "perf.sh: pane $id did not finish its output" >&2; exit 1; }
done

if [ "${PERF_MEMORY:-0}" != 1 ]; then
	start_app
fi

if [ "${PERF_MEMORY:-0}" = 1 ]; then
	[ -n "$app_pid" ] || { echo "perf.sh: memory workload requires app" >&2; exit 1; }
	TMPDIR="$tmp/apptmp/" python3 -I scripts/perf_memory.py run "$tmp" "$app_pid" "$kerod_pid" "$panes" "$busy"
	exit
fi

if [ "${PERF_GUI:-0}" = 1 ]; then
	[ -n "$app_pid" ] || { echo "perf.sh: GUI workload requires app" >&2; exit 1; }
	TMPDIR="$tmp/apptmp/" python3 -I scripts/perf_memory.py gui "$tmp" "$app_pid" "$kerod_pid" "$panes" "$busy"
	exit
fi

echo "perf.sh: settling ${settle}s, then ${idle}s of idle" >&2
sleep "$settle"
pids=$kerod_pid${app_pid:+ $app_pid}
$probe sample $pids >"$tmp/t0.json"
log_idle_from=$(($(wc -l <"$tmp/app.log") + 1))
sleep "$idle"
$probe sample $pids >"$tmp/t1.json"
log_idle_to=$(($(wc -l <"$tmp/app.log")))

frames=
if [ -n "$app_pid" ]; then
	# Output streaming into the pane the app shows (app.json's selected
	# session, else the first), for the frame times.
	log_busy_from=$((log_idle_to + 1))
	shown=$(python3 -I -c 'import json, sys; print(json.load(open(sys.argv[1])).get("selected", ""))' "$KERO_HOME/app.json" 2>/dev/null || true)
	grep -qx "$shown" "$tmp/sessions" 2>/dev/null || shown=$(head -n 1 "$tmp/sessions")
	"$bin" send --enter "$shown" go >/dev/null
	sleep $((busy + 1))
	log_busy_to=$(($(wc -l <"$tmp/app.log")))
	frames="--idle-frames $tmp/app.log:$log_idle_from:$log_idle_to --busy-frames $tmp/app.log:$log_busy_from:$log_busy_to"
fi

$probe report --panes "$panes" --fed "$fed" --kerod "$kerod_pid" \
	--base "$tmp/base.json" --t0 "$tmp/t0.json" --t1 "$tmp/t1.json" \
	${app_pid:+--app "$app_pid"} --app-reason "$app_reason" $frames
