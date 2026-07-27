function json(value: unknown): string {
  const result = JSON.stringify(value)
  if (result === undefined) {
    throw new Error("not JSON")
  }
  return result
}

export function rejectLoneSurrogate(value: string): void {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index)
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(index + 1)
      if (!Number.isInteger(next) || next < 0xdc00 || next > 0xdfff) {
        throw new Error("lone surrogate")
      }
      index += 1
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      throw new Error("lone surrogate")
    }
  }
}

const isRecord = (value: unknown): value is Record<string, unknown> => typeof value === "object" && value !== null && !Array.isArray(value)

export function canonicalizeJSON(value: unknown): string {
  if (value === null || typeof value === "boolean") {
    return json(value)
  }
  if (typeof value === "string") {
    rejectLoneSurrogate(value)
    return json(value)
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) {
      throw new Error("non-finite JSON number")
    }
    return json(value)
  }
  if (Array.isArray(value)) {
    return `[${value.map(canonicalizeJSON).join(",")}]`
  }
  if (isRecord(value)) {
    return `{${Object.keys(value).sort().map((key) => {
      rejectLoneSurrogate(key)
      return `${json(key)}:${canonicalizeJSON(value[key])}`
    }).join(",")}}`
  }
  throw new Error("not JSON")
}
