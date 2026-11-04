#!/bin/sh
# ARTEX supervisor start script (Linux / macOS / Docker ENTRYPOINT)
#
# Usage:
#   ./start.sh                       run in the foreground (Ctrl-C to stop)
#   nohup ./start.sh >artex.log 2>&1 &   run in the background, persistently
#   ./start.sh -addr :9000           extra arguments are passed through to artex unchanged
#
# It does exactly one thing: run artex, and after the process exits decide from the exit code whether to start it again.
#
#   0      normal stop by the user      -> leave the loop
#   75     the program requested a restart  -> rerun immediately (the page's "one-click update" or "rollback" was clicked)
#   other  crash                        -> rerun after a backoff (1->2->4... up to 60 seconds)
#
# Downloading, SHA256 verification and binary swapping are deliberately NOT done here: that logic would have to be
# written twice, once for sh and once for bat, and it is precisely the step that must never go wrong. If a binary that
# cannot run were swapped in, this script would faithfully restart it over and over, and the user would have to
# rescue it by hand on the machine. So verification/swapping all stay in Go (the selfupdate package) and are done by
# artex itself at startup; the script stays dumb on purpose.
set -u

cd "$(dirname "$0")" || exit 1

BIN=./artex
[ -x "$BIN" ] || { echo "[artex] executable not found: $BIN" >&2; exit 1; }

RESTART_CODE=75
MAX_DELAY=60

child=0
stopping=0

# Forward the stop signal to the artex process itself.
#
# This is required under Docker: docker stop only sends SIGTERM to PID 1 (that is, this script),
# not to child processes. Without forwarding, artex never receives the signal, cannot shut down
# gracefully, and is killed by SIGKILL after 10 seconds, cutting running tasks off halfway.
forward() {
	stopping=1
	if [ "$child" -ne 0 ]; then
		kill -TERM "$child" 2>/dev/null || true
	fi
}
trap forward INT TERM

delay=1
while :; do
	"$BIN" "$@" &
	child=$!

	# A signal interrupts wait and makes it return >128. At that point the child is actually still shutting down gracefully,
	# so wait must be called once more to get its real exit code.
	wait "$child"
	code=$?
	if [ "$code" -gt 128 ]; then
		wait "$child"
		code=$?
	fi
	child=0

	if [ "$stopping" -eq 1 ]; then
		echo "[artex] stopped"
		exit 0
	fi

	case "$code" in
		0)
			echo "[artex] exited normally"
			exit 0
			;;
		"$RESTART_CODE")
			# Update/rollback is ready: after the rerun, artex completes the swap at startup (see selfupdate.Bootstrap).
			echo "[artex] restart requested (applying new version)..."
			delay=1
			;;
		*)
			echo "[artex] exited abnormally (code=$code), restarting in ${delay}s" >&2
			sleep "$delay"
			delay=$((delay * 2))
			[ "$delay" -gt "$MAX_DELAY" ] && delay=$MAX_DELAY
			;;
	esac
done
