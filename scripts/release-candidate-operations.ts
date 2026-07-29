import { join } from "node:path"
import { digest, primaryNames, regularBytes, scannedNames } from "./release-candidate-data"
import { readCandidateInputs } from "./release-candidate-inputs"
import { withReleaseTransaction } from "./release-candidate-transaction"

export async function assembleReleaseCandidate(request: {
  readonly outputDirectory: string
  readonly producerDirectory: string
  readonly relationDirectory: string
  readonly schemaPath: string
}): Promise<void> {
  await withReleaseTransaction(async (transaction) => {
    const inputs = await readCandidateInputs(request.producerDirectory, request.relationDirectory, request.schemaPath)
    await transaction.atomicDirectory(request.outputDirectory, ".candidate-", async (stage) => {
      for (const receipt of inputs.producers) {
        await Bun.write(join(stage, receipt.name), await regularBytes(join(request.producerDirectory, receipt.name)))
      }
      for (const name of scannedNames(inputs.version)) {
        await Bun.write(join(stage, `${name}.spdx.json`), await regularBytes(join(request.relationDirectory, `${name}.spdx.json`)))
      }
      const names = [...primaryNames(inputs.version), ...scannedNames(inputs.version).map((name) => `${name}.spdx.json`)].sort()
      const lines = await Promise.all(names.map(async (name) => `${digest(await regularBytes(join(stage, name)))}  ${name}`))
      await Bun.write(join(stage, "SHA256SUMS"), `${lines.join("\n")}\n`)
    })
  })
}
