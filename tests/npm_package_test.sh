#!/bin/sh

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
version=$(tr -d '\r\n' <"$root/VERSION")
archive="$root/dist/npm/k911mipt-opencode-agent-managed-bash-$version.tgz"
stage=$(mktemp -d)
trap 'rm -rf "$stage"; rm -f "$archive"; rmdir "$root/dist/npm" 2>/dev/null || true' EXIT HUP INT TERM

SOURCE_MANIFEST_PATH="$root/plugins/opencode/package.json" bun -e '
const manifest = JSON.parse(await Bun.file(process.env.SOURCE_MANIFEST_PATH).text())
if (manifest.name !== "@k911mipt/opencode-agent-managed-bash" || manifest.private !== true) {
  throw new TypeError("source plugin workspace is not the required private identity")
}
'

make --no-print-directory -C "$root" npm-package

test -f "$archive"
tar -tzf "$archive" | sort >"$stage/files"
printf '%s\n' \
  'package/LICENSE' \
  'package/README.md' \
  'package/THIRD_PARTY_NOTICES.txt' \
  'package/managed-bash.js' \
  'package/package.json' >"$stage/expected-files"
cmp "$stage/expected-files" "$stage/files"
tar -xzf "$archive" -C "$stage"

MANIFEST_PATH="$stage/package/package.json" EXPECTED_VERSION="$version" bun -e '
const manifest = JSON.parse(await Bun.file(process.env.MANIFEST_PATH).text())
const expected = {
  name: "@k911mipt/opencode-agent-managed-bash",
  version: process.env.EXPECTED_VERSION,
  type: "module",
  main: "./managed-bash.js",
  exports: "./managed-bash.js",
  license: "MIT",
  repository: { type: "git", url: "git+https://github.com/k911mipt/agent-managed-bash.git" },
  publishConfig: { access: "public" },
}
if (JSON.stringify(manifest) !== JSON.stringify(expected)) throw new TypeError("unexpected generated package manifest")
if ("dependencies" in manifest) throw new TypeError("public package has runtime dependencies")
'

PACKAGE_PATH="$stage/package/managed-bash.js" EXPECTED_VERSION="$version" bun -e '
const plugin = await import(process.env.PACKAGE_PATH)
if (JSON.stringify(Object.keys(plugin).sort()) !== JSON.stringify(["ManagedBashPlugin"])) {
  throw new TypeError("public package exposes unexpected exports")
}
if (typeof plugin.ManagedBashPlugin !== "function") throw new TypeError("public package does not export a plugin function")
if (plugin.ManagedBashPlugin.managedBashReleaseVersion !== process.env.EXPECTED_VERSION) {
  throw new TypeError("public package plugin release differs from VERSION")
}
'

mismatch_root="$stage/mismatch"
mkdir -p "$mismatch_root/plugins/opencode/dist" "$mismatch_root/packaging" "$mismatch_root/dist/npm"
cp "$root/VERSION" "$root/README.md" "$root/LICENSE" "$mismatch_root/"
cp "$root/packaging/THIRD_PARTY_NOTICES.txt" "$mismatch_root/packaging/THIRD_PARTY_NOTICES.txt"
printf '%s\n' \
  'export function ManagedBashPlugin() {}' \
  'Object.defineProperty(ManagedBashPlugin, "managedBashReleaseVersion", { value: "9.9.9" })' \
  >"$mismatch_root/plugins/opencode/dist/managed-bash.js"
sentinel="$mismatch_root/dist/npm/k911mipt-opencode-agent-managed-bash-$version.tgz"
printf '%s\n' sentinel >"$sentinel"
if (cd "$mismatch_root" && bun "$root/scripts/package-opencode-plugin.ts") >"$stage/mismatch.log" 2>&1; then
  printf '%s\n' 'mismatched plugin bundle unexpectedly packaged' >&2
  exit 1
fi
printf '%s\n' sentinel >"$stage/sentinel"
cmp "$stage/sentinel" "$sentinel"
grep -F 'does not match VERSION' "$stage/mismatch.log" >/dev/null

pack_failure_root="$stage/pack-failure"
pack_failure_tmp="$pack_failure_root/tmp"
pack_failure_bin="$pack_failure_root/bin"
mkdir -p "$pack_failure_root/plugins/opencode/dist" "$pack_failure_root/packaging" "$pack_failure_root/dist/npm" "$pack_failure_tmp" "$pack_failure_bin"
cp "$root/VERSION" "$root/README.md" "$root/LICENSE" "$pack_failure_root/"
cp "$root/packaging/THIRD_PARTY_NOTICES.txt" "$pack_failure_root/packaging/THIRD_PARTY_NOTICES.txt"
cp "$root/plugins/opencode/dist/managed-bash.js" "$pack_failure_root/plugins/opencode/dist/managed-bash.js"
pack_failure_sentinel="$pack_failure_root/dist/npm/k911mipt-opencode-agent-managed-bash-$version.tgz"
printf '%s\n' sentinel >"$pack_failure_sentinel"
printf '%s\n' \
  '#!/bin/sh' \
  'if test "$1" = pm && test "$2" = pack; then exit 23; fi' \
  'exec "$REAL_BUN" "$@"' \
  >"$pack_failure_bin/bun"
chmod +x "$pack_failure_bin/bun"
if (cd "$pack_failure_root" && REAL_BUN="$(command -v bun)" PATH="$pack_failure_bin:$PATH" TMPDIR="$pack_failure_tmp" "$(command -v bun)" "$root/scripts/package-opencode-plugin.ts") >"$stage/pack-failure.log" 2>&1; then
  printf '%s\n' 'ordinary pack failure unexpectedly succeeded' >&2
  exit 1
fi
cmp "$stage/sentinel" "$pack_failure_sentinel"
test -z "$(ls "$pack_failure_tmp")"
test -z "$(ls "$pack_failure_root/dist/npm" | grep '^\.')"
grep -F 'pack public plugin failed' "$stage/pack-failure.log" >/dev/null

for interrupt_signal in TERM INT; do
  interrupt_root="$stage/interrupt-$interrupt_signal"
  interrupt_tmp="$interrupt_root/tmp"
  interrupt_bin="$interrupt_root/bin"
  interrupt_ready="$interrupt_root/ready"
  interrupt_fake_pids="$interrupt_root/fake-pids"
  mkdir -p "$interrupt_root/plugins/opencode/dist" "$interrupt_root/packaging" "$interrupt_root/dist/npm" "$interrupt_tmp" "$interrupt_bin"
  mkfifo "$interrupt_ready"
  cp "$root/VERSION" "$root/README.md" "$root/LICENSE" "$interrupt_root/"
  cp "$root/packaging/THIRD_PARTY_NOTICES.txt" "$interrupt_root/packaging/THIRD_PARTY_NOTICES.txt"
  cp "$root/plugins/opencode/dist/managed-bash.js" "$interrupt_root/plugins/opencode/dist/managed-bash.js"
  interrupt_sentinel="$interrupt_root/dist/npm/k911mipt-opencode-agent-managed-bash-$version.tgz"
  printf '%s\n' sentinel >"$interrupt_sentinel"
  interrupt_sentinel_hash=$(shasum -a 256 "$interrupt_sentinel" | cut -d ' ' -f1)
  printf '%s\n' \
    '#!/bin/sh' \
    'if test "$1" = pm && test "$2" = pack; then' \
    "  sh -c 'trap \"\" TERM; exec sleep 300' &" \
    '  descendant_pid=$!' \
    '  printf "%s %s\\n" "$$" "$descendant_pid" >"$FAKE_PIDS_PATH"' \
    '  printf "ready\\n" >"$READY_FIFO"' \
    '  wait "$descendant_pid"' \
    'fi' \
    'exec "$REAL_BUN" "$@"' \
    >"$interrupt_bin/bun"
  chmod +x "$interrupt_bin/bun"
  (cd "$interrupt_root" && exec env REAL_BUN="$(command -v bun)" PATH="$interrupt_bin:$PATH" TMPDIR="$interrupt_tmp" READY_FIFO="$interrupt_ready" FAKE_PIDS_PATH="$interrupt_fake_pids" "$(command -v bun)" "$root/scripts/package-opencode-plugin.ts") >"$stage/interrupt-$interrupt_signal.log" 2>&1 &
  pack_pid=$!
  IFS= read -r ready <"$interrupt_ready"
  start_seconds=$(date +%s)
  (sleep 5; kill -KILL "$pack_pid" 2>/dev/null || true) &
  watchdog_pid=$!
  kill -"$interrupt_signal" "$pack_pid"
  if wait "$pack_pid"; then
    printf '%s\n' "$interrupt_signal interruption unexpectedly succeeded" >&2
    exit 1
  else
    interrupt_status=$?
  fi
  if test "$interrupt_signal" = TERM; then expected_status=143; else expected_status=130; fi
  test "$interrupt_status" -eq "$expected_status"
  end_seconds=$(date +%s)
  kill -TERM "$watchdog_pid" 2>/dev/null || true
  test $((end_seconds - start_seconds)) -lt 6
  set -- $(tr -d '\r\n' <"$interrupt_fake_pids")
  wrapper_pid=$1
  descendant_pid=$2
  if kill -0 "$wrapper_pid" 2>/dev/null || kill -0 "$descendant_pid" 2>/dev/null; then
    kill -TERM "$wrapper_pid" "$descendant_pid" 2>/dev/null || true
    printf '%s\n' "$interrupt_signal interruption left a pack process alive" >&2
    exit 1
  fi
  cmp "$stage/sentinel" "$interrupt_sentinel"
  test -z "$(ls "$interrupt_tmp")"
  test -z "$(ls "$interrupt_root/dist/npm" | grep '^\.')"
  test "$interrupt_sentinel_hash" = "$(shasum -a 256 "$interrupt_sentinel" | cut -d ' ' -f1)"
  printf '%s\n' "interrupt=$interrupt_signal status=$interrupt_status elapsed_seconds=$((end_seconds - start_seconds)) wrapper_pid=$wrapper_pid descendant_pid=$descendant_pid sentinel_sha256=$interrupt_sentinel_hash"
done

unexpected_root="$stage/unexpected"
mkdir -p "$unexpected_root/plugins/opencode/dist" "$unexpected_root/packaging"
cp "$root/VERSION" "$root/README.md" "$root/LICENSE" "$unexpected_root/"
cp "$root/packaging/THIRD_PARTY_NOTICES.txt" "$unexpected_root/packaging/THIRD_PARTY_NOTICES.txt"
cp "$root/plugins/opencode/dist/managed-bash.js" "$unexpected_root/plugins/opencode/dist/managed-bash.js"
if PACKAGE_SCRIPT="$root/scripts/package-opencode-plugin.ts" PACKAGE_ROOT="$unexpected_root" PACKAGE_STAGE="$unexpected_root/stage" bun -e '
const { stagePublicPluginPackage, assertPublicPluginStage } = await import(process.env.PACKAGE_SCRIPT)
await stagePublicPluginPackage({ root: process.env.PACKAGE_ROOT, stage: process.env.PACKAGE_STAGE })
await Bun.write(`${process.env.PACKAGE_STAGE}/unexpected`, "unexpected\n")
await assertPublicPluginStage(process.env.PACKAGE_STAGE)
' >"$stage/unexpected.log" 2>&1; then
  printf '%s\n' 'unexpected staged file was accepted' >&2
  exit 1
fi
grep -F 'unexpected staged package files: unexpected' "$stage/unexpected.log" >/dev/null
