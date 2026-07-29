import { rejectLoneSurrogate } from "./release-candidate-canonical"

type JSONValue = null | boolean | number | string | readonly JSONValue[] | { readonly [key: string]: JSONValue }

class StrictJSONParser {
  #index = 0
  readonly #source: string

  constructor(source: string) {
    this.#source = source
  }

  parse(): JSONValue {
    this.skipWhitespace()
    const value = this.parseValue()
    this.skipWhitespace()
    if (this.#index !== this.#source.length) {
      throw new Error("invalid JSON trailing input")
    }
    return value
  }

  private parseValue(): JSONValue {
    const character = this.#source[this.#index]
    if (character === "{") {
      return this.parseObject()
    }
    if (character === "[") {
      return this.parseArray()
    }
    if (character === "\"") {
      return this.parseString()
    }
    if (character === "t") {
      this.expect("true")
      return true
    }
    if (character === "f") {
      this.expect("false")
      return false
    }
    if (character === "n") {
      this.expect("null")
      return null
    }
    if (character === "-" || (character !== undefined && character >= "0" && character <= "9")) {
      return this.parseNumber()
    }
    throw new Error("invalid JSON value")
  }

  private parseObject(): { readonly [key: string]: JSONValue } {
    const result: Record<string, JSONValue> = {}
    const keys = new Set<string>()
    this.#index += 1
    this.skipWhitespace()
    if (this.consume("}")) {
      return result
    }
    while (true) {
      if (this.#source[this.#index] !== "\"") {
        throw new Error("invalid JSON object key")
      }
      const key = this.parseString()
      if (keys.has(key)) {
        throw new Error("duplicate JSON object key")
      }
      keys.add(key)
      this.skipWhitespace()
      this.expect(":")
      this.skipWhitespace()
      Object.defineProperty(result, key, { configurable: true, enumerable: true, value: this.parseValue(), writable: true })
      this.skipWhitespace()
      if (this.consume("}")) {
        return result
      }
      this.expect(",")
      this.skipWhitespace()
    }
  }

  private parseArray(): readonly JSONValue[] {
    const result: JSONValue[] = []
    this.#index += 1
    this.skipWhitespace()
    if (this.consume("]")) {
      return result
    }
    while (true) {
      result.push(this.parseValue())
      this.skipWhitespace()
      if (this.consume("]")) {
        return result
      }
      this.expect(",")
      this.skipWhitespace()
    }
  }

  private parseString(): string {
    this.expect("\"")
    let result = ""
    while (true) {
      const character = this.#source[this.#index]
      if (character === undefined) {
        throw new Error("unterminated JSON string")
      }
      this.#index += 1
      if (character === "\"") {
        rejectLoneSurrogate(result)
        return result
      }
      if (character === "\\") {
        result += this.parseEscape()
      } else {
        if (character.charCodeAt(0) < 0x20) {
          throw new Error("invalid JSON string control character")
        }
        result += character
      }
    }
  }

  private parseEscape(): string {
    const escape = this.#source[this.#index]
    if (escape === undefined) {
      throw new Error("unterminated JSON escape")
    }
    this.#index += 1
    const simpleEscapes: Readonly<Record<string, string>> = {
      "\"": "\"",
      "\\": "\\",
      "/": "/",
      b: "\b",
      f: "\f",
      n: "\n",
      r: "\r",
      t: "\t",
    }
    const simple = simpleEscapes[escape]
    if (simple !== undefined) {
      return simple
    }
    if (escape !== "u") {
      throw new Error("invalid JSON escape")
    }
    const hex = this.#source.slice(this.#index, this.#index + 4)
    if (!/^[0-9a-fA-F]{4}$/.test(hex)) {
      throw new Error("invalid JSON unicode escape")
    }
    this.#index += 4
    return String.fromCharCode(Number.parseInt(hex, 16))
  }

  private parseNumber(): number {
    const start = this.#index
    this.consume("-")
    if (!this.consume("0")) {
      this.consumeDigits(true)
    }
    if (this.consume(".")) {
      this.consumeDigits(true)
    }
    const exponent = this.#source[this.#index]
    if (exponent === "e" || exponent === "E") {
      this.#index += 1
      const sign = this.#source[this.#index]
      if (sign === "+" || sign === "-") {
        this.#index += 1
      }
      this.consumeDigits(true)
    }
    const value = Number(this.#source.slice(start, this.#index))
    if (!Number.isFinite(value)) {
      throw new Error("non-finite JSON number")
    }
    return value
  }

  private consumeDigits(required: boolean): void {
    const start = this.#index
    while (true) {
      const character = this.#source[this.#index]
      if (character === undefined || character < "0" || character > "9") {
        break
      }
      this.#index += 1
    }
    if (required && start === this.#index) {
      throw new Error("invalid JSON number")
    }
  }

  private consume(value: string): boolean {
    if (this.#source.startsWith(value, this.#index)) {
      this.#index += value.length
      return true
    }
    return false
  }

  private expect(value: string): void {
    if (!this.consume(value)) {
      throw new Error("invalid JSON syntax")
    }
  }

  private skipWhitespace(): void {
    while (true) {
      const character = this.#source[this.#index]
      if (character !== " " && character !== "\n" && character !== "\r" && character !== "\t") {
        return
      }
      this.#index += 1
    }
  }
}

export function parseStrictJSON(source: string): JSONValue {
  return new StrictJSONParser(source).parse()
}
