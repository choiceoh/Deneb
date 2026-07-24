#!/usr/bin/env node
/**
 * Bake gateway origin + bridge token into the Even Hub plugin package:
 *   - public/runtime-config.json  (loaded at boot; not a secret in git)
 *   - app.json network whitelist   (Even Hub requires explicit origins)
 *
 * Usage:
 *   DENEB_EVEN_G2_BASE_URL=http://100.x.x.x:18789 \
 *   DENEB_EVEN_G2_BRIDGE_TOKEN=… \
 *   node scripts/prepare-config.mjs
 *
 * Prefer QR ?seed= for personal sideload so the token never ships in .ehpk.
 * For private builds on a trusted phone, baking is acceptable.
 */
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const baseUrl = (process.env.DENEB_EVEN_G2_BASE_URL || '').trim().replace(/\/$/, '')
const token = (process.env.DENEB_EVEN_G2_BRIDGE_TOKEN || '').trim()

if (!baseUrl) {
  console.error('DENEB_EVEN_G2_BASE_URL is required (e.g. http://100.x.x.x:18789)')
  process.exit(1)
}

let origin
try {
  origin = new URL(baseUrl).origin
} catch {
  console.error(`invalid DENEB_EVEN_G2_BASE_URL: ${baseUrl}`)
  process.exit(1)
}

const publicDir = join(root, 'public')
mkdirSync(publicDir, { recursive: true })
const runtimePath = join(publicDir, 'runtime-config.json')
writeFileSync(
  runtimePath,
  JSON.stringify({ baseUrl, token }, null, 2) + '\n',
  'utf8',
)
console.log(`wrote ${runtimePath}`)

const appPath = join(root, 'app.json')
const app = JSON.parse(readFileSync(appPath, 'utf8'))
const perms = Array.isArray(app.permissions) ? app.permissions : []
let network = perms.find((p) => p && p.name === 'network')
if (!network) {
  network = {
    name: 'network',
    desc: 'Fetches a short Deneb glance summary from the private gateway.',
    whitelist: [],
  }
  perms.push(network)
}
const whitelist = new Set(Array.isArray(network.whitelist) ? network.whitelist : [])
whitelist.add(origin)
network.whitelist = [...whitelist]
app.permissions = perms
writeFileSync(appPath, JSON.stringify(app, null, 2) + '\n', 'utf8')
console.log(`updated ${appPath} whitelist → ${network.whitelist.join(', ')}`)
