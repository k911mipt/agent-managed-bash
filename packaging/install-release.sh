#!/bin/sh

set -eu

fail() {
	printf '%s\n' "$1" >&2
	exit 1
}

if test "$#" -ne 1; then
	fail 'usage: install-release.sh vMAJOR.MINOR.PATCH'
fi

tag=$1
if ! printf '%s\n' "$tag" | LC_ALL=C grep -E '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' >/dev/null; then
	fail 'release tag must be an exact vMAJOR.MINOR.PATCH value'
fi

uname_command=${MANAGED_BASH_TEST_UNAME:-uname}
host_os=$("$uname_command" -s) || fail 'cannot determine operating system'
host_arch=$("$uname_command" -m) || fail 'cannot determine architecture'
case "$host_os/$host_arch" in
	Darwin/arm64) target=darwin-arm64 ;;
	Darwin/x86_64) target=darwin-amd64 ;;
	Linux/arm64|Linux/aarch64) target=linux-arm64 ;;
	Linux/amd64|Linux/x86_64) target=linux-amd64 ;;
	*) fail "unsupported host: $host_os/$host_arch" ;;
esac

version=${tag#v}
archive_name="agent-managed-bash-$version-$target.tar.gz"
archive_root=${archive_name%.tar.gz}

test_override=false
if test "${MANAGED_BASH_RELEASE_BASE_URL+x}" = x; then
	base_url=$MANAGED_BASH_RELEASE_BASE_URL
	if ! printf '%s\n' "$base_url" | LC_ALL=C grep -E '^http://127\.0\.0\.1:[0-9][0-9]*(/[A-Za-z0-9._-]+)*$' >/dev/null; then
		fail 'MANAGED_BASH_RELEASE_BASE_URL is limited to loopback HTTP test fixtures'
	fi
	test_override=true
else
	base_url="https://github.com/k911mipt/agent-managed-bash/releases/download/$tag"
fi

command -v curl >/dev/null 2>&1 || fail 'curl is required'
work=
cleanup() {
	if test -n "${work:-}"; then
		rm -rf "$work"
	fi
}
trap 'status=$?; trap - 0 HUP INT TERM; cleanup; exit "$status"' 0
trap 'exit 1' HUP INT TERM
work=$(mktemp -d) || fail 'cannot create temporary directory'
work=$(CDPATH= cd "$work" && pwd -P) || fail 'cannot resolve temporary directory'

download() {
	url=$1
	destination=$2
	if test "$test_override" = true; then
		curl --fail --location --silent --show-error --proto =http --proto-redir =http --connect-timeout 15 --max-time 300 --output "$destination" "$url" || fail "download failed: $url"
	else
		curl --fail --location --silent --show-error --proto =https --proto-redir =https --connect-timeout 15 --max-time 300 --output "$destination" "$url" || fail "download failed: $url"
	fi
}

ustar_name_field() {
	encoded=$(LC_ALL=C printf '%s' "$1" | od -An -v -tx1 | tr -d ' \n') || return 1
	while test "${#encoded}" -lt 200; do encoded="${encoded}00"; done
	test "${#encoded}" = 200 || return 1
	printf '%s\n' "$encoded"
}

ustar_size() {
	LC_ALL=C awk -v field="$1" -v limit="$2" 'BEGIN { if (length(field) != 24 || (substr(field, 23, 2) != "00" && substr(field, 23, 2) != "20")) exit 1; value = 0; for (position = 1; position <= 22; position += 2) { byte = substr(field, position, 2); if (byte !~ /^3[0-7]$/) exit 1; value = value * 8 + substr(byte, 2, 1) } if (value > limit) exit 1; printf "%.0f\n", value }'
}

validate_ustar_headers() {
	raw_tar=$1; expected=$2
	tar_bytes=$(wc -c <"$raw_tar" | tr -d ' ') || return 1; test "$((tar_bytes % 512))" -eq 0 || return 1
	total_blocks=$((tar_bytes / 512)); block=0; raw_seen="$work/raw-members"; : >"$raw_seen"
	while :; do
		dd if="$raw_tar" of="$work/ustar-header" bs=512 count=1 skip="$block" 2>/dev/null || return 1; test "$(wc -c <"$work/ustar-header" | tr -d ' ')" = 512 || return 1
		header=$(od -An -v -tx1 "$work/ustar-header" | tr -d ' \n') || return 1; test "${#header}" = 1024 || return 1
		case "$header" in
			*[!0]*) ;;
			*)
				end_block=$((block + 1))
				test "$((end_block + 1))" -eq "$total_blocks" || return 1
				dd if="$raw_tar" of="$work/ustar-end" bs=512 count=1 skip="$end_block" 2>/dev/null || return 1
				end_header=$(od -An -v -tx1 "$work/ustar-end" | tr -d ' \n') || return 1
				case "$end_header" in *[!0]*) return 1 ;; esac
				break
				;;
		esac
		name_field=$(printf '%s\n' "$header" | cut -c 1-200); size_field=$(printf '%s\n' "$header" | cut -c 249-272)
		type_field=$(printf '%s\n' "$header" | cut -c 313-314)
		link_field=$(printf '%s\n' "$header" | cut -c 315-514)
		magic_field=$(printf '%s\n' "$header" | cut -c 515-526)
		version_field=$(printf '%s\n' "$header" | cut -c 527-530)
		prefix_field=$(printf '%s\n' "$header" | cut -c 691-1000)
		test "$magic_field" = 757374617200 && test "$version_field" = 3030 || return 1
		case "$link_field$prefix_field" in *[!0]*) return 1 ;; esac
		matched=
		while IFS= read -r candidate; do
			candidate_field=$(ustar_name_field "$candidate") || return 1
			if test "$name_field" = "$candidate_field"; then matched=$candidate; break; fi
		done <"$expected"
		test -n "$matched" || return 1
		if grep -F -x "$matched" "$raw_seen" >/dev/null 2>&1; then return 1; fi
		case "$matched" in */) test "$type_field" = 35 || return 1 ;; *) case "$type_field" in 00|30) ;; *) return 1 ;; esac ;; esac
		size=$(ustar_size "$size_field" "$tar_bytes") || return 1
		printf '%s\n' "$matched" >>"$raw_seen"
		block=$((block + 1 + (size + 511) / 512))
		test "$block" -lt "$total_blocks" || return 1
	done
	LC_ALL=C sort "$raw_seen" >"$work/raw-members.sorted"
	cmp "$work/expected-members.sorted" "$work/raw-members.sorted" >/dev/null
}

checksums="$work/SHA256SUMS"
archive="$work/$archive_name"
download "$base_url/SHA256SUMS" "$checksums"
download "$base_url/$archive_name" "$archive"

expected_checksum=$(LC_ALL=C awk -v wanted="$archive_name" '
{
	if (length($0) <= 66 || substr($0, 65, 2) != "  ") {
		invalid = 1
		next
	}
	digest = substr($0, 1, 64)
	filename = substr($0, 67)
	if (length(digest) != 64 || digest !~ /^[0123456789abcdef]+$/ || filename !~ /^[A-Za-z0-9._+-]+$/) {
		invalid = 1
		next
	}
	if (filename == wanted) {
		count++
		expected = digest
	}
}
END {
	if (invalid || count != 1) {
		exit 1
	}
	print expected
}
' "$checksums") || fail 'SHA256SUMS does not contain one canonical checksum for the selected archive'

if command -v sha256sum >/dev/null 2>&1; then
	actual_checksum=$(sha256sum "$archive" | awk '{print $1}') || fail 'cannot calculate archive checksum'
else
	actual_checksum=$(shasum -a 256 "$archive" | awk '{print $1}') || fail 'cannot calculate archive checksum'
fi
test "$actual_checksum" = "$expected_checksum" || fail 'archive checksum mismatch'

members="$work/members"
expected_members="$work/expected-members"
errors="$work/tar-errors"
printf '%s\n' \
	"$archive_root/" \
	"$archive_root/bin/" \
	"$archive_root/lib/" \
	"$archive_root/lib/opencode/" \
	"$archive_root/manifest.json" \
	"$archive_root/LICENSE" \
	"$archive_root/README.md" \
	"$archive_root/THIRD_PARTY_NOTICES.txt" \
	"$archive_root/bin/managed-bash" \
	"$archive_root/install.sh" \
	"$archive_root/lib/opencode/managed-bash.js" \
	"$archive_root/uninstall.sh" >"$expected_members"

if ! tar -P -tzf "$archive" >"$members" 2>"$errors"; then
	fail 'cannot list archive members'
fi
test ! -s "$errors" || fail 'archive member listing reported unsafe paths'
while IFS= read -r member; do
	case "$member" in
		/*|../*|*/../*|*/..|..|*'//'*) fail 'archive contains an unsafe member path' ;;
	esac
done <"$members"
test "$(wc -l <"$members" | tr -d ' ')" = 12 || fail 'archive member count is invalid'
LC_ALL=C sort "$members" >"$work/members.sorted"
LC_ALL=C sort "$expected_members" >"$work/expected-members.sorted"
cmp "$work/expected-members.sorted" "$work/members.sorted" >/dev/null || fail 'archive member layout is invalid'

if ! tar -P -tvzf "$archive" >"$work/types" 2>"$errors"; then
	fail 'cannot inspect archive member types'
fi
test ! -s "$errors" || fail 'archive type listing reported unsafe paths'
if ! LC_ALL=C awk -v root="$archive_root/" '
BEGIN {
	directories[root] = 1
	directories[root "bin/"] = 1
	directories[root "lib/"] = 1
	directories[root "lib/opencode/"] = 1
	files[root "manifest.json"] = 1
	files[root "LICENSE"] = 1
	files[root "README.md"] = 1
	files[root "THIRD_PARTY_NOTICES.txt"] = 1
	files[root "bin/managed-bash"] = 1
	files[root "install.sh"] = 1
	files[root "lib/opencode/managed-bash.js"] = 1
	files[root "uninstall.sh"] = 1
}
{
	type = substr($0, 1, 1)
	entry = $NF
	if (type == "d" && directories[entry]) {
		directories_seen[entry]++
	} else if (type == "-" && files[entry]) {
		files_seen[entry]++
	} else {
		invalid = 1
	}
}
END {
	for (entry in directories) if (directories_seen[entry] != 1) invalid = 1
	for (entry in files) if (files_seen[entry] != 1) invalid = 1
	exit invalid
}
' "$work/types"; then
	fail 'archive member types are invalid'
fi

command -v gzip >/dev/null 2>&1 || fail 'gzip is required'
raw_tar="$work/archive.tar"
gzip -dc "$archive" >"$raw_tar" || fail 'cannot decompress archive'
if ! validate_ustar_headers "$raw_tar" "$expected_members"; then
	fail 'archive USTAR headers are invalid'
fi

extract="$work/extract"
mkdir "$extract"
if ! tar -P -xzf "$archive" -C "$extract" \
	"$archive_root/manifest.json" \
	"$archive_root/LICENSE" \
	"$archive_root/README.md" \
	"$archive_root/THIRD_PARTY_NOTICES.txt" \
	"$archive_root/bin/managed-bash" \
	"$archive_root/install.sh" \
	"$archive_root/lib/opencode/managed-bash.js" \
	"$archive_root/uninstall.sh"; then
	fail 'cannot extract approved archive members'
fi
physical_extract=$(CDPATH= cd "$extract" && pwd -P) || fail 'cannot resolve extraction directory'
physical_root=$(CDPATH= cd "$extract/$archive_root" && pwd -P) || fail 'cannot resolve extracted archive root'
case "$physical_root" in
	"$physical_extract"/*) ;;
	*) fail 'extracted archive root escapes the temporary directory' ;;
esac
while IFS= read -r member; do
	case "$member" in
		*/)
			physical_path=$(CDPATH= cd "$extract/$member" && pwd -P) || fail 'expected archive directory is missing'
			;;
		*)
			parent=${member%/*}
			physical_path=$(CDPATH= cd "$extract/$parent" && pwd -P) || fail 'expected archive file parent is missing'
			test -f "$extract/$member" || fail 'expected archive file is missing'
			;;
	esac
	case "$physical_path" in
		"$physical_extract"|"$physical_extract"/*) ;;
		*) fail 'extracted archive member escapes the temporary directory' ;;
	esac
done <"$expected_members"

sh "$physical_root/install.sh"
