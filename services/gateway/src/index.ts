import { loadConfig } from './config.js'
import { startGateway } from './gateway.js'

const config = loadConfig()
const gateway = startGateway(config)

gateway.server.on('listening', () => {
  process.stdout.write(`muhan gateway listening on ${gateway.address()}\n`)
})

let shuttingDown = false
const shutdown = (signal: string) => {
  if (shuttingDown) return
  shuttingDown = true
  process.stdout.write(`received ${signal}; draining gateway\n`)
  void gateway.close().then(() => process.exit(0)).catch((error: unknown) => {
    process.stderr.write(`gateway shutdown failed: ${error instanceof Error ? error.stack ?? error.message : String(error)}\n`)
    process.exit(1)
  })
}

process.on('SIGTERM', () => shutdown('SIGTERM'))
process.on('SIGINT', () => shutdown('SIGINT'))
