#!/bin/sh

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
: "${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH is required}"
stage=$(mktemp -d)
stage=$(CDPATH= cd -- "$stage" && pwd -P)
server_pid=
trap 'status=$?; trap - 0 HUP INT TERM; if test -n "$server_pid"; then kill -TERM "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi; chmod -R u+w "$stage" 2>/dev/null || true; rm -rf "$stage"; exit "$status"' 0
trap 'exit 1' HUP INT TERM

version=$(tr -d '\r\n' <"$root/VERSION")
tag="v$version"
case $(uname -s)/$(uname -m) in
	Linux/x86_64|Linux/amd64) target=linux-amd64 ;;
	Linux/aarch64|Linux/arm64) target=linux-arm64 ;;
	Darwin/x86_64) target=darwin-amd64 ;;
	Darwin/arm64) target=darwin-arm64 ;;
	*) printf '%s\n' 'unsupported native test host' >&2; exit 1 ;;
esac
archive_name="agent-managed-bash-$version-$target.tar.gz"
archive_root="${archive_name%.tar.gz}"
native_archive="$root/dist/$archive_name"
fixtures="$stage/fixtures"
ready="$stage/server-ready"
request_ready="$stage/request-ready"
mkdir -p "$fixtures"

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

prepare_release() {
	scenario=$1
	archive=$2
	checksum_kind=$3
	release="$fixtures/$scenario/$tag"
	mkdir -p "$release"
	if test "$archive" != none; then
		ln "$archive" "$release/$archive_name"
	fi
	case $checksum_kind in
		valid) printf '%s  %s\n' "$(sha256_file "$archive")" "$archive_name" >"$release/SHA256SUMS" ;;
		bad) printf '%064d  %s\n' 0 "$archive_name" >"$release/SHA256SUMS" ;;
		duplicate) checksum=$(sha256_file "$archive"); printf '%s  %s\n%s  %s\n' "$checksum" "$archive_name" "$checksum" "$archive_name" >"$release/SHA256SUMS" ;;
		malformed) printf '%s\n' "not-a-checksum  $archive_name" >"$release/SHA256SUMS" ;;
		missing) : ;;
		*) printf '%s\n' "unknown checksum fixture: $checksum_kind" >&2; exit 1 ;;
	esac
}

make_malformed_archive() {
	kind=$1
	archive="$stage/$kind.tar.gz"
	bun "$root/tests/public_install_fixture_archive.mjs" "$kind" "$archive" "$archive_root" "$stage/outside-$kind"
	printf '%s\n' "$archive"
}

bun "$root/tests/public_install_fixture_server.mjs" "$fixtures" "$ready" "$request_ready" >"$stage/server.out" 2>"$stage/server.err" &
server_pid=$!
for attempt in 1 2 3 4 5; do
	if test -s "$ready"; then break; fi
	if ! kill -0 "$server_pid" 2>/dev/null; then printf '%s\n' 'fixture server stopped before readiness' >&2; exit 1; fi
	sleep 1
done
test -s "$ready"
port=$(tr -d '\r\n' <"$ready")
base="http://127.0.0.1:$port"

prepare_release valid "$native_archive" valid
prepare_release bad-checksum "$native_archive" bad
prepare_release duplicate-checksum "$native_archive" duplicate
prepare_release malformed-checksum "$native_archive" malformed
prepare_release missing-checksum "$native_archive" missing
prepare_release interrupted "$native_archive" valid
prepare_release hang "$native_archive" valid
prepare_release traversal "$(make_malformed_archive traversal)" valid
prepare_release absolute "$(make_malformed_archive absolute)" valid
prepare_release duplicate-root "$(make_malformed_archive duplicate-root)" valid
prepare_release escaping-link "$(make_malformed_archive escaping-link)" valid
prepare_release fifo "$(make_malformed_archive fifo)" valid
prepare_release hidden-linkname "$(make_malformed_archive hidden-linkname)" valid
prepare_release non-octal-size "$(make_malformed_archive non-octal-size)" valid
prepare_release truncated "$(make_malformed_archive truncated)" valid
mkdir -p "$fixtures/not-found/$tag"

failure_home="$stage/failure-home"
failure_data="$stage/failure-data"
failure_bin="$stage/failure-bin"
mkdir -p "$failure_home" "$failure_data" "$failure_bin"

assert_no_install_state() {
	test ! -e "$failure_data/agent-managed-bash"
	test ! -e "$failure_bin/managed-bash"
}

run_failure() {
	scenario=$1
	if HOME="$failure_home" XDG_DATA_HOME="$failure_data" MANAGED_BASH_BIN_DIR="$failure_bin" \
		MANAGED_BASH_RELEASE_BASE_URL="$base/$scenario/$tag" sh "$root/packaging/install-release.sh" "$tag" >"$stage/$scenario.log" 2>&1; then
		printf '%s\n' "fixture unexpectedly installed: $scenario" >&2
		exit 1
	fi
	assert_no_install_state
}

for bad_tag in v01.0.0 0.1.0 v1.0 v1.0.0.0; do
	if HOME="$failure_home" XDG_DATA_HOME="$failure_data" MANAGED_BASH_BIN_DIR="$failure_bin" sh "$root/packaging/install-release.sh" "$bad_tag" >"$stage/tag.log" 2>&1; then
		printf '%s\n' "malformed tag unexpectedly accepted: $bad_tag" >&2
		exit 1
	fi
	assert_no_install_state
done
if HOME="$failure_home" XDG_DATA_HOME="$failure_data" MANAGED_BASH_BIN_DIR="$failure_bin" sh "$root/packaging/install-release.sh" "$tag" extra >"$stage/arguments.log" 2>&1; then
	printf '%s\n' 'extra argument unexpectedly accepted' >&2
	exit 1
fi
assert_no_install_state

fake_uname="$stage/fake-uname"
printf '%s\n' '#!/bin/sh' 'case $1 in -s) printf %s "Other" ;; -m) printf %s "unknown" ;; esac' >"$fake_uname"
chmod +x "$fake_uname"
if MANAGED_BASH_TEST_UNAME="$fake_uname" sh "$root/packaging/install-release.sh" "$tag" >"$stage/host.log" 2>&1; then
	printf '%s\n' 'unsupported host unexpectedly accepted' >&2
	exit 1
fi

fake_curl="$stage/curl"
printf '%s\n' \
	'#!/bin/sh' \
	'protocol=' \
	'redirect_protocol=' \
	'url=' \
	'while test "$#" -gt 0; do' \
	'  case $1 in' \
	'    --proto) protocol=$2; shift 2 ;;' \
	'    --proto-redir) redirect_protocol=$2; shift 2 ;;' \
	'    --output) shift 2 ;;' \
	'    --connect-timeout|--max-time) shift 2 ;;' \
	'    *) url=$1; shift ;;' \
	'  esac' \
	'done' \
	'test "$protocol" = =https || exit 1' \
	'test "$redirect_protocol" = =https || exit 1' \
	"test \"\$url\" = \"https://github.com/k911mipt/agent-managed-bash/releases/download/$tag/SHA256SUMS\" || exit 1" \
	'exit 47' >"$fake_curl"
chmod +x "$fake_curl"
if PATH="$stage:$PATH" sh "$root/packaging/install-release.sh" "$tag" >"$stage/https.log" 2>&1; then
	printf '%s\n' 'HTTPS redirect guard unexpectedly accepted a failed download' >&2
	exit 1
fi
if MANAGED_BASH_RELEASE_BASE_URL='http://example.invalid' sh "$root/packaging/install-release.sh" "$tag" >"$stage/url.log" 2>&1; then
	printf '%s\n' 'non-loopback HTTP override unexpectedly accepted' >&2
	exit 1
fi

hidden_marker="$stage/hidden-marker"
if HOME="$failure_home" XDG_DATA_HOME="$failure_data" MANAGED_BASH_BIN_DIR="$failure_bin" MARKER="$hidden_marker" \
	MANAGED_BASH_RELEASE_BASE_URL="$base/hidden-linkname/$tag" sh "$root/packaging/install-release.sh" "$tag" >"$stage/hidden-linkname.log" 2>&1; then
	test -f "$hidden_marker" || { printf '%s\n' 'hidden regular linkname archive delegated without the expected marker' >&2; exit 1; }
	printf '%s\n' 'hidden regular linkname archive unexpectedly delegated and wrote the marker' >&2
	exit 1
fi
test ! -e "$hidden_marker"
test ! -e "$stage/outside-hidden-linkname"
assert_no_install_state

for scenario in bad-checksum duplicate-checksum malformed-checksum missing-checksum not-found interrupted traversal absolute duplicate-root escaping-link fifo non-octal-size truncated; do
	run_failure "$scenario"
done
test ! -e "$stage/outside-traversal"
test ! -e "$stage/outside-absolute"
test ! -e "$stage/outside-escaping-link"

happy_home="$stage/happy-home"
happy_data="$stage/happy-data"
happy_bin="$stage/happy-bin"
mkdir -p "$happy_home" "$happy_data" "$happy_bin"
HOME="$happy_home" XDG_DATA_HOME="$happy_data" MANAGED_BASH_BIN_DIR="$happy_bin" \
	MANAGED_BASH_RELEASE_BASE_URL="$base/valid/$tag" sh "$root/packaging/install-release.sh" "$tag"
response=$(printf '%s' '{"schema_version":1,"action":"version"}' | "$happy_bin/managed-bash" version)
RESPONSE="$response" VERSION="$version" TARGET="$target" bun -e '
const response = JSON.parse(process.env.RESPONSE)
if (!response.ok || response.action !== "version") process.exit(1)
const result = response.result
if (result.product !== "managed-bash" || result.binary_version !== process.env.VERSION || result.protocol_version !== 1 || `${result.os}-${result.architecture}` !== process.env.TARGET) process.exit(1)
'
current="$happy_data/agent-managed-bash/current"
current_target=$(readlink "$current")

assert_stale_state() {
	test "$(readlink "$current")" = "$current_target"
	stale_response=$(printf '%s' '{"schema_version":1,"action":"version"}' | "$happy_bin/managed-bash" version)
	RESPONSE="$stale_response" VERSION="$version" TARGET="$target" bun -e '
const response = JSON.parse(process.env.RESPONSE)
if (!response.ok || response.result.binary_version !== process.env.VERSION || `${response.result.os}-${response.result.architecture}` !== process.env.TARGET) process.exit(1)
'
}

for scenario in bad-checksum duplicate-checksum malformed-checksum missing-checksum not-found interrupted traversal absolute duplicate-root escaping-link fifo non-octal-size truncated; do
	HOME="$happy_home" XDG_DATA_HOME="$happy_data" MANAGED_BASH_BIN_DIR="$happy_bin" \
		MANAGED_BASH_RELEASE_BASE_URL="$base/$scenario/$tag" sh "$root/packaging/install-release.sh" "$tag" >"$stage/stale-$scenario.log" 2>&1 && exit 1
	assert_stale_state
done
for bad_tag in v01.0.0 0.1.0 v1.0 v1.0.0.0; do
	HOME="$happy_home" XDG_DATA_HOME="$happy_data" MANAGED_BASH_BIN_DIR="$happy_bin" sh "$root/packaging/install-release.sh" "$bad_tag" >"$stage/stale-tag.log" 2>&1 && exit 1
	assert_stale_state
done
HOME="$happy_home" XDG_DATA_HOME="$happy_data" MANAGED_BASH_BIN_DIR="$happy_bin" sh "$root/packaging/install-release.sh" "$tag" extra >"$stage/stale-arguments.log" 2>&1 && exit 1
assert_stale_state
MANAGED_BASH_TEST_UNAME="$fake_uname" HOME="$happy_home" XDG_DATA_HOME="$happy_data" MANAGED_BASH_BIN_DIR="$happy_bin" sh "$root/packaging/install-release.sh" "$tag" >"$stage/stale-host.log" 2>&1 && exit 1
assert_stale_state
PATH="$stage:$PATH" HOME="$happy_home" XDG_DATA_HOME="$happy_data" MANAGED_BASH_BIN_DIR="$happy_bin" sh "$root/packaging/install-release.sh" "$tag" >"$stage/stale-https.log" 2>&1 && exit 1
assert_stale_state
if HOME="$happy_home" XDG_DATA_HOME="$happy_data" MANAGED_BASH_BIN_DIR="$happy_bin" MARKER="$hidden_marker" \
	MANAGED_BASH_RELEASE_BASE_URL="$base/hidden-linkname/$tag" sh "$root/packaging/install-release.sh" "$tag" >"$stage/stale-hidden-linkname.log" 2>&1; then
	printf '%s\n' 'hidden regular linkname archive unexpectedly changed existing state' >&2
	exit 1
fi
test ! -e "$hidden_marker"
test ! -e "$stage/outside-hidden-linkname"
assert_stale_state

HOME="$failure_home" XDG_DATA_HOME="$failure_data" MANAGED_BASH_BIN_DIR="$failure_bin" \
	MANAGED_BASH_RELEASE_BASE_URL="$base/hang/$tag" sh "$root/packaging/install-release.sh" "$tag" >"$stage/hang.log" 2>&1 &
hang_pid=$!
for attempt in 1 2 3 4 5; do
	if test -s "$request_ready"; then break; fi
	if ! kill -0 "$hang_pid" 2>/dev/null; then printf '%s\n' 'bootstrap exited before hung fixture request' >&2; exit 1; fi
	sleep 1
done
test -s "$request_ready"
kill -TERM "$hang_pid"
if wait "$hang_pid"; then printf '%s\n' 'interrupted bootstrap unexpectedly succeeded' >&2; exit 1; fi
assert_no_install_state
set -- "$stage"/managed-bash-release.*
test ! -e "$1"
