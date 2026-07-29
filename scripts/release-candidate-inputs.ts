import { readdir } from "node:fs/promises"
import { join } from "node:path"
import {
  primaryNames,
  readJSON,
  scannedNames,
  validateProducerReceipt,
  validateRelationReceipt,
  validateSpdxDocuments,
  verifyProducerReceipt,
  verifyRelationReceipt,
} from "./release-candidate-data"
import type { ProducerReceipt, RelationReceipt } from "./release-candidate-data"

export type CandidateInputs = {
  readonly producers: readonly ProducerReceipt[]
  readonly relations: readonly RelationReceipt[]
  readonly version: string
}

async function exactDirectory(path: string, names: readonly string[]): Promise<void> {
  const actual = await readdir(path)
  if (actual.length !== names.length || actual.some((name) => !names.includes(name))) {
    throw new Error(`unexpected files in ${path}`)
  }
}

async function readProducers(directory: string): Promise<{ readonly receipts: readonly ProducerReceipt[]; readonly version: string }> {
  const receiptFiles = (await readdir(directory)).filter((name) => name.endsWith(".receipt.json"))
  const firstName = receiptFiles[0]
  if (firstName === undefined) {
    throw new Error("missing producer receipt")
  }
  const first = validateProducerReceipt(await readJSON(join(directory, firstName)))
  const names = primaryNames(first.version)
  await exactDirectory(directory, names.flatMap((name) => [name, `${name}.receipt.json`]))
  const receipts = await Promise.all(names.map(async (name) => validateProducerReceipt(await readJSON(join(directory, `${name}.receipt.json`)))))
  if (receipts.some((receipt, index) => receipt.name !== names[index] || receipt.version !== first.version || receipt.commit !== first.commit)) {
    throw new Error("producer receipt identity mismatch")
  }
  await Promise.all(receipts.map(async (receipt) => verifyProducerReceipt({ assetPath: join(directory, receipt.name), receipt })))
  return { receipts, version: first.version }
}

async function readRelations(
  directory: string,
  producerDirectory: string,
  producers: readonly ProducerReceipt[],
  schemaPath: string,
): Promise<readonly RelationReceipt[]> {
  const names = scannedNames(producers[0]?.version ?? "")
  await exactDirectory(directory, names.flatMap((name) => [`${name}.spdx.json`, `${name}.spdx.json.relation.json`]))
  const relations = await Promise.all(names.map(async (name, index) => {
    const subject = producers[index]
    if (subject === undefined) {
      throw new Error("missing producer receipt")
    }
    const predicatePath = join(directory, `${name}.spdx.json`)
    const relation = validateRelationReceipt(await readJSON(join(directory, `${name}.spdx.json.relation.json`)))
    await verifyRelationReceipt({ assetPath: join(producerDirectory, subject.name), predicatePath, relation, schemaPath, subject })
    return relation
  }))
  await validateSpdxDocuments(await Promise.all(names.map(async (name) => readJSON(join(directory, `${name}.spdx.json`)))), schemaPath)
  return relations
}

export async function readCandidateInputs(producerDirectory: string, relationDirectory: string, schemaPath: string): Promise<CandidateInputs> {
  const producerResult = await readProducers(producerDirectory)
  const relations = await readRelations(relationDirectory, producerDirectory, producerResult.receipts, schemaPath)
  return { producers: producerResult.receipts, relations, version: producerResult.version }
}
