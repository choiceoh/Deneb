#!/usr/bin/env node
// Hub bridge adapted from @page-agent/mcp (MIT, Alibaba page-agent).
// Adds: configurable bind host, loopback-only WebSocket hub.
import { readFileSync } from 'node:fs'
import http from 'node:http'
import { fileURLToPath } from 'node:url'
import { WebSocketServer } from 'ws'

const EXT_ID = 'akldabonmimlicnjlflnapfeklbfemhj'
const STORE_URL = `https://chromewebstore.google.com/detail/page-agent-ext/${EXT_ID}`

const launcherTemplate = readFileSync(
	fileURLToPath(new URL('./launcher.html', import.meta.url)),
	'utf-8',
)

function isLoopback(addr) {
	if (!addr) return false
	return (
		addr === '127.0.0.1' ||
		addr === '::1' ||
		addr === '::ffff:127.0.0.1' ||
		addr.endsWith('127.0.0.1')
	)
}

/**
 * HTTP + WebSocket bridge to the Page Agent extension hub tab.
 * - GET / serves the launcher (triggers extension to open hub)
 * - WS (loopback only) carries execute/stop + result/error
 */
export class HubBridge {
	/** @type {number} */
	port
	/** @type {string} */
	host
	/** @type {http.Server} */
	#httpServer
	/** @type {WebSocketServer} */
	#wss
	/** @type {import('ws').WebSocket | null} */
	#hub = null
	/** @type {{ resolve: (r: {success: boolean, data: string}) => void, reject: (e: Error) => void } | null} */
	#pendingTask = null

	/** @param {{ port: number, host?: string }} opts */
	constructor({ port, host = '127.0.0.1' }) {
		this.port = port
		this.host = host
		this.#httpServer = http.createServer()
		this.#wss = new WebSocketServer({ noServer: true })

		this.#httpServer.on('upgrade', (req, socket, head) => {
			const remote = req.socket.remoteAddress
			if (!isLoopback(remote)) {
				socket.write('HTTP/1.1 403 Forbidden\r\n\r\n')
				socket.destroy()
				return
			}
			this.#wss.handleUpgrade(req, socket, head, (ws) => {
				this.#wss.emit('connection', ws, req)
			})
		})
		this.#wss.on('connection', (ws) => this.#onConnection(ws))
	}

	/** @returns {http.Server} */
	get httpServer() {
		return this.#httpServer
	}

	/** Serve launcher HTML on the given response. */
	serveLauncher(res) {
		const html = launcherTemplate
			.replaceAll('__EXT_ID__', EXT_ID)
			.replaceAll('__STORE_URL__', STORE_URL)
			.replaceAll('__WS_PORT__', String(this.port))
		res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
		res.end(html)
	}

	/** @returns {Promise<void>} */
	async start() {
		return new Promise((resolve, reject) => {
			this.#httpServer.on('error', (/** @type {NodeJS.ErrnoException} */ err) => {
				if (err.code === 'EADDRINUSE') {
					reject(
						new Error(
							`Port ${this.port} is in use. Another Page Agent bridge may be running.`,
						),
					)
				} else {
					reject(err)
				}
			})
			this.#httpServer.listen(this.port, this.host, () => {
				console.error(
					`[page-agent-bridge] HTTP on http://${this.host}:${this.port} (hub WS: loopback only)`,
				)
				resolve()
			})
		})
	}

	get connected() {
		return this.#hub?.readyState === 1
	}

	get busy() {
		return this.#pendingTask !== null
	}

	/**
	 * @param {string} task
	 * @param {Record<string, unknown>} [config]
	 * @returns {Promise<{success: boolean, data: string}>}
	 */
	async executeTask(task, config) {
		if (!this.connected) {
			throw new Error(
				'Hub is not connected. Open http://127.0.0.1:' +
					this.port +
					'/ in Chrome with the Page Agent extension installed.',
			)
		}
		if (this.#pendingTask) throw new Error('Agent is already running a task.')

		return new Promise((resolve, reject) => {
			this.#pendingTask = { resolve, reject }
			this.#hub.send(JSON.stringify({ type: 'execute', task, config }))
		})
	}

	stopTask() {
		if (this.connected) {
			this.#hub.send(JSON.stringify({ type: 'stop' }))
		}
	}

	/** @param {import('ws').WebSocket} ws */
	#onConnection(ws) {
		if (this.#hub && this.#hub.readyState === 1) {
			ws.close(4000, 'Another hub is already connected')
			return
		}

		this.#hub = ws
		console.error('[page-agent-bridge] Hub connected')

		ws.on('message', (/** @type {Buffer} */ rawData) => {
			/** @type {{ type: string, success?: boolean, data?: string, message?: string }} */
			let msg
			try {
				msg = JSON.parse(rawData.toString('utf-8'))
			} catch {
				return
			}

			if (msg.type === 'result') {
				this.#pendingTask?.resolve({ success: msg.success ?? false, data: msg.data ?? '' })
				this.#pendingTask = null
			} else if (msg.type === 'error') {
				this.#pendingTask?.reject(new Error(msg.message ?? 'Unknown error from hub'))
				this.#pendingTask = null
			}
		})

		ws.on('close', () => {
			console.error('[page-agent-bridge] Hub disconnected')
			if (this.#hub === ws) this.#hub = null
			if (this.#pendingTask) {
				this.#pendingTask.reject(new Error('Hub disconnected while task was running'))
				this.#pendingTask = null
			}
		})
	}
}
