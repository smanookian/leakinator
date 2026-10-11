#!/bin/sh
# Runs an installed leakinator on the test videos and checks the results.
#   tools/smoke.sh [path/to/leakinator]
set -u
bin=${1:-leakinator}
cd "$(dirname "$0")/.." || exit 2
v=testdata/videos
fails=0

# expect NAME EXIT_CODE TEXT_IN_OUTPUT -- ARGS…
expect() {
	name=$1 want=$2 text=$3
	shift 4
	out=$("$bin" "$@" --no-color 2>&1)
	code=$?
	if [ "$code" = "$want" ] && printf '%s' "$out" | grep -qF -- "$text"; then
		echo "ok    $name"
	else
		echo "FAIL  $name (exit $code, want $want; want text: $text)"
		printf '%s\n' "$out" | sed 's/^/      /'
		fails=$((fails + 1))
	fi
}

"$bin" --version || { echo "FAIL  $bin does not run"; exit 1; }
expect ".env key found"   1 "00:05-00:07   MY_API_TOKEN" -- $v/env_key.mp4 --env testdata/test.env --only secrets
expect "pattern keys"     1 "9 secrets found"            -- $v/pattern_keys.mp4 --only secrets
expect "clean video"      0 "All good."                  -- $v/clean.mp4 --env testdata/test.env
expect "small text"       0 "00:04-00:09   text too small" -- $v/small_text.mp4 --only text
expect "loudness"         0 "Clipping at 00:02-00:04"   -- $v/loud_audio.mp4 --only audio

[ "$fails" = 0 ] && echo "All smoke tests passed." || { echo "$fails smoke test(s) failed."; exit 1; }
