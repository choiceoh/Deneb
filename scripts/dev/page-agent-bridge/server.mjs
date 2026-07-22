#!/usr/bin/env node
/**
 * Deneb Page Agent bridge — exposes the user's Chrome (via Page Agent extension)
 * to the gateway over Tailscale HTTP.
 *
 * Protocol (token required on /v1/*):
 *   GET  /v1/status              → { connected, busy }
 *   POST /v1/execute { task }    → { success, data }
 *   POST /v1/stop                → { ok: true }
 *   GET  /                       → launcher (open in Chrome once)
 *
 * Env:
 *   DENEB_BROWSER_TOKEN  required shared secret (Bearer / X-Deneb-Browser-Token)
 *   PORT                 default 38401
 *   HOST                 default 0.0.0.0 (reach from srv4 via Tailscale)
 *   LLM_BASE_URL / LLM_API_KEY / LLM_MODEL_NAME  optional, forwarded to the hub
 *   OPEN_LAUNCHER=0      skip auto-opening the launcher in the local browser
 */
import { exec } from 'node:child_process'
import { platform } from 'node:os'

import { HubBridge } from './hub-bridge.js'

const port = parseInt(process.env.PORT || '38401', 10)
const host = process.env.HOST || '0.0.0.0'
const token = (process.env.DENEB_BROWSER_TOKEN || '').trim()

if (!token) {
	console.error('[page-agent-bridge] DENEB_BROWSER_TOKEN is required')
	process.exit(1)
}

/** @type {Record<string, string>} */
const llmConfig = {}
if (process.env.LLM_BASE_URL) llmConfig.baseURL = process.env.LLM_BASE_URL
if (process.env.LLM_MODEL_NAME) llmConfig.model = process.env.LLM_MODEL_NAME
if (process.env.LLM_API_KEY) llmConfig.apiKey = process.env.LLM_API_KEY

const hub = new HubBridge({ port, host })

function authorized(req) {
	const auth = req.headers.authorization || ''
	const bearer = auth.startsWith('Bearer ') ? auth.slice(7).trim() : ''
	const header = (req.headers['x-deneb-browser-token'] || '').toString().trim()
	return bearer === token || header === token
}

function readJSON(req) {
	return new Promise((resolve, reject) => {
		const chunks = []
		req.on('data', (c) => chunks.push(c))
		req.on('end', () => {
			const raw = Buffer.concat(chunks).toString('utf8')
			if (!raw.trim()) {
				resolve({})
				return
			}
			try {
				resolve(JSON.parse(raw))
			} catch (err) {
				reject(err)
			}
		})
		req.on('error', reject)
	})
}

function sendJSON(res, status, body) {
	const data = JSON.stringify(body)
	res.writeHead(status, {
		'Content-Type': 'application/json; charset=utf-8',
		'Content-Length': Buffer.byteLength(data),
	})
	res.end(data)
}

hub.httpServer.on('request', async (req, res) => {
	const url = new URL(req.url || '/', `http://${req.headers.host || 'localhost'}`)
	const path = url.pathname

	if (req.method === 'GET' && (path === '/' || path === '')) {
		hub.serveLauncher(res)
		return
	}

	if (path === '/health' || path === '/v1/health') {
		sendJSON(res, 200, { ok: true, connected: hub.connected, busy: hub.busy })
		return
	}

	if (!path.startsWith('/v1/')) {
		sendJSON(res, 404, { error: 'not found' })
		return
	}

	if (!authorized(req)) {
		sendJSON(res, 401, { error: 'unauthorized' })
		return
	}

	try {
		if (req.method === 'GET' && path === '/v1/status') {
			sendJSON(res, 200, { connected: hub.connected, busy: hub.busy })
			return
		}
		if (req.method === 'POST' && path === '/v1/stop') {
			hub.stopTask()
			sendJSON(res, 200, { ok: true })
			return
		}
		if (req.method === 'POST' && path === '/v1/execute') {
			const body = await readJSON(req)
			const task = typeof body.task === 'string' ? body.task.trim() : ''
			if (!task) {
				sendJSON(res, 400, { error: 'task is required' })
				return
			}
			/** @type {Record<string, unknown> | undefined} */
			let config = Object.keys(llmConfig).length > 0 ? { ...llmConfig } : undefined
			if (body.config && typeof body.config === 'object') {
				config = { ...(config || {}), ...body.config }
			}
			const result = await hub.executeTask(task, config)
			sendJSON(res, 200, result)
			return
		}
		sendJSON(res, 404, { error: 'not found' })
	} catch (err) {
		// Keep client responses generic — Error messages can embed stack frames.
		console.error('[page-agent-bridge] request error:', err instanceof Error ? err.message : err)
		sendJSON(res, 500, { error: 'internal error' })
	}
})

await hub.start()

const launcherURL = `http://127.0.0.1:${port}/`
if (process.env.OPEN_LAUNCHER !== '0') {
	const cmd =
		platform() === 'darwin' ? 'open' : platform() === 'win32' ? 'start ""' : 'xdg-open'
	exec(`${cmd} "${launcherURL}"`, (err) => {
		if (err) {
			console.error(`[page-agent-bridge] Could not open browser: ${err.message}`)
			console.error(`[page-agent-bridge] Open manually: ${launcherURL}`)
		}
	})
}

console.error(`[page-agent-bridge] ready — launcher ${launcherURL}`)
console.error(
	`[page-agent-bridge] gateway: DENEB_BROWSER_URL=http://<this-tailscale-ip>:${port} DENEB_BROWSER_TOKEN=<same token>`,
)
