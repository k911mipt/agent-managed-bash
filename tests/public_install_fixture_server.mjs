import { join } from "node:path"

const [root, ready, requestReady] = process.argv.slice(2)

if (root === undefined || ready === undefined || requestReady === undefined) {
  throw new TypeError("expected fixture root, readiness file, and request readiness file")
}

const server = Bun.serve({
  hostname: "127.0.0.1",
  port: 0,
  async fetch(request) {
    const segments = new URL(request.url).pathname.split("/").filter(Boolean)
    if (segments.length !== 3 || segments.some((segment) => !/^[A-Za-z0-9._-]+$/.test(segment) || segment === "..")) {
      return new Response("not found", { status: 404 })
    }

    const [scenario, tag, name] = segments
    if (scenario === undefined || tag === undefined || name === undefined) {
      return new Response("not found", { status: 404 })
    }
    if (scenario === "hang" && name.endsWith(".tar.gz")) {
      await Bun.write(requestReady, "requested\n")
      return new Promise(() => {})
    }

    const file = Bun.file(join(root, scenario, tag, name))
    if (!(await file.exists())) {
      return new Response("not found", { status: 404 })
    }
    if (scenario === "interrupted" && name.endsWith(".tar.gz")) {
      return new Response(
        new ReadableStream({
          start(controller) {
            controller.enqueue(new Uint8Array([0x1f, 0x8b, 0x08, 0x00]))
            queueMicrotask(() => controller.error(new Error("interrupted fixture response")))
          },
        }),
        { headers: { "content-length": "4096" } },
      )
    }
    return new Response(file)
  },
})

await Bun.write(ready, `${server.port}\n`)
process.on("SIGINT", () => {
  server.stop(true)
  process.exit(0)
})
process.on("SIGTERM", () => {
  server.stop(true)
  process.exit(0)
})
await new Promise(() => {})
