import { writeFile } from "node:fs/promises"

const [kind, output, root, outside] = process.argv.slice(2)

if (kind === undefined || output === undefined || root === undefined || outside === undefined) {
  throw new TypeError("expected fixture kind, output, archive root, and outside path")
}

const entries = entriesFor(kind, root, outside)
const blocks = entries.reduce((count, entry) => count + 1 + Math.ceil(entry.data.length / 512), kind === "truncated" ? 0 : 2)
const archive = new Uint8Array(blocks * 512)
let offset = 0

for (const entry of entries) {
  archive.set(headerFor(entry), offset)
  offset += 512
  archive.set(entry.data, offset)
  offset += Math.ceil(entry.data.length / 512) * 512
}

await writeFile(output, Bun.gzipSync(archive))

function entriesFor(kind, root, outside) {
  switch (kind) {
    case "traversal":
      return [{ name: "../escape", type: "0", linkname: "", data: new Uint8Array() }]
    case "absolute":
      return [{ name: outside, type: "0", linkname: "", data: new Uint8Array() }]
    case "duplicate-root":
      return [
        { name: `${root}/`, type: "5", linkname: "", data: new Uint8Array() },
        { name: `${root}/`, type: "5", linkname: "", data: new Uint8Array() },
      ]
    case "escaping-link":
      return [{ name: `${root}/bin/managed-bash`, type: "2", linkname: "../../escape", data: new Uint8Array() }]
    case "fifo":
      return [{ name: `${root}/bin/managed-bash`, type: "6", linkname: "", data: new Uint8Array() }]
    case "hidden-linkname":
      return exactEntries(root)
    case "non-octal-size":
      return [{ name: `${root}/install.sh`, type: "0", linkname: "", sizeField: "0000000000x\0", data: new Uint8Array() }]
    case "truncated":
      return [{ name: `${root}/install.sh`, type: "0", linkname: "", data: new Uint8Array() }]
    default:
      throw new TypeError(`unsupported archive fixture kind: ${kind}`)
  }
}

function exactEntries(root) {
  const encode = (value) => new TextEncoder().encode(value)
  return [
    { name: `${root}/`, type: "5", linkname: "", data: new Uint8Array() },
    { name: `${root}/bin/`, type: "5", linkname: "", data: new Uint8Array() },
    { name: `${root}/lib/`, type: "5", linkname: "", data: new Uint8Array() },
    { name: `${root}/lib/opencode/`, type: "5", linkname: "", data: new Uint8Array() },
    { name: `${root}/manifest.json`, type: "0", linkname: "", data: encode("{}\n") },
    { name: `${root}/LICENSE`, type: "0", linkname: "", data: encode("license\n") },
    { name: `${root}/README.md`, type: "0", linkname: "", data: encode("readme\n") },
    { name: `${root}/THIRD_PARTY_NOTICES.txt`, type: "0", linkname: "", data: encode("notices\n") },
    { name: `${root}/bin/managed-bash`, type: "0", linkname: "", data: encode("binary\n") },
    { name: `${root}/install.sh`, type: "0", linkname: "../../outside", data: encode(": \"${MARKER:?}\"\nprintf '%s\\n' delegated >\"$MARKER\"\n") },
    { name: `${root}/lib/opencode/managed-bash.js`, type: "0", linkname: "", data: encode("plugin\n") },
    { name: `${root}/uninstall.sh`, type: "0", linkname: "", data: encode("uninstall\n") },
  ]
}

function headerFor(entry) {
  const header = new Uint8Array(512)
  writeString(header, 0, 100, entry.name)
  writeOctal(header, 100, 8, entry.type === "5" ? 0o755 : 0o644)
  writeOctal(header, 108, 8, 0)
  writeOctal(header, 116, 8, 0)
  writeOctal(header, 124, 12, entry.data.length)
  if (entry.sizeField !== undefined) {
    writeString(header, 124, 12, entry.sizeField)
  }
  writeOctal(header, 136, 12, 0)
  header.fill(0x20, 148, 156)
  header[156] = entry.type.charCodeAt(0)
  writeString(header, 157, 100, entry.linkname)
  writeString(header, 257, 6, "ustar\0")
  writeString(header, 263, 2, "00")
  writeOctal(header, 329, 8, 0)
  writeOctal(header, 337, 8, 0)
  const checksum = header.reduce((total, byte) => total + byte, 0)
  writeString(header, 148, 6, checksum.toString(8).padStart(6, "0"))
  header[154] = 0
  header[155] = 0x20
  return header
}

function writeString(target, offset, size, value) {
  const encoded = new TextEncoder().encode(value)
  if (encoded.length > size) {
    throw new RangeError(`tar field is too long: ${value}`)
  }
  target.set(encoded, offset)
}

function writeOctal(target, offset, size, value) {
  const encoded = `${value.toString(8).padStart(size - 1, "0")}\0`
  writeString(target, offset, size, encoded)
}
