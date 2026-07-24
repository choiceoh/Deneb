#!/usr/bin/env node
/** Print a base64url seed for QR bootstrap: ?seed=<output> */
const baseUrl = (process.env.DENEB_EVEN_G2_BASE_URL || '').trim().replace(/\/$/, '')
const token = (process.env.DENEB_EVEN_G2_BRIDGE_TOKEN || '').trim()
if (!baseUrl || !token) {
  console.error('Need DENEB_EVEN_G2_BASE_URL and DENEB_EVEN_G2_BRIDGE_TOKEN')
  process.exit(1)
}
const json = JSON.stringify({ baseUrl, token })
const seed = Buffer.from(json, 'utf8')
  .toString('base64')
  .replace(/\+/g, '-')
  .replace(/\//g, '_')
  .replace(/=+$/g, '')
console.log(seed)
