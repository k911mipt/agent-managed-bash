export function deletedSessionID(event: unknown): string | undefined {
  if (!isRecord(event) || event["type"] !== "session.deleted" || !isRecord(event["properties"])) {
    return undefined
  }
  const info = event["properties"]["info"]
  return isRecord(info) && typeof info["id"] === "string" ? info["id"] : undefined
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}
