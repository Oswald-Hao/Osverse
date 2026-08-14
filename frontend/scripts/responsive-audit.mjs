import { spawn } from 'node:child_process'
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const widths = [901, 960, 1024, 1053]
const expectedLabels = ['总览', 'API 配置', '安装记录', '设置']
const previewPort = 4178

const chromeCandidates = [
  process.env.OSVERSE_CHROME,
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser',
].filter(Boolean)
const chromePath = chromeCandidates.find(existsSync)

if (!chromePath) {
  throw new Error('Responsive audit requires Chrome or Chromium')
}

const profile = mkdtempSync(join(tmpdir(), 'osverse-responsive-audit-'))
const preview = spawn(
  process.platform === 'win32' ? 'npm.cmd' : 'npm',
  ['exec', 'vite', '--', 'preview', '--host', '127.0.0.1', '--port', String(previewPort), '--strictPort'],
  {
    cwd: process.cwd(),
    detached: process.platform !== 'win32',
    stdio: 'ignore',
  },
)
const chrome = spawn(
  chromePath,
  [
    '--headless=new',
    '--no-sandbox',
    '--disable-gpu',
    '--disable-dev-shm-usage',
    '--no-first-run',
    '--no-default-browser-check',
    '--remote-debugging-port=0',
    `--user-data-dir=${profile}`,
    'about:blank',
  ],
  { detached: process.platform !== 'win32', stdio: 'ignore' },
)

function terminate(child) {
  if (!child.pid || child.killed) return
  try {
    if (process.platform === 'win32') child.kill('SIGTERM')
    else process.kill(-child.pid, 'SIGTERM')
  } catch (error) {
    if (error?.code !== 'ESRCH') throw error
  }
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds))
}

async function waitFor(check, description) {
  const deadline = Date.now() + 20_000
  let lastError
  while (Date.now() < deadline) {
    try {
      const value = await check()
      if (value) return value
    } catch (error) {
      lastError = error
    }
    await delay(50)
  }
  throw new Error(`Timed out waiting for ${description}`, { cause: lastError })
}

function scanSnapshot() {
  return {
    scannedAt: '2026-08-13T08:05:06Z',
    system: {
      distribution: 'Ubuntu',
      version: '24.04',
      architecture: 'x86_64',
      shell: '/bin/bash',
      supported: true,
      unsupportedReason: '',
    },
    components: [
      {
        id: 'codex-cli',
        name: 'Codex CLI',
        category: 'Core CLI',
        status: 'conflict',
        installations: [
          {
            path: '/home/test/.local/bin/a-very-long-codex-installation-name-without-a-break',
            resolvedPath: '/opt/osverse/tools/a-very-long-codex-resolved-target-without-a-break',
            version: '2.0.0',
            source: 'path',
            managed: false,
          },
        ],
        message: '检测到多个安装位置',
        minimumOS: 'Ubuntu 20.04',
      },
      {
        id: 'claude-desktop',
        name: 'Claude Desktop',
        category: 'Desktop Applications',
        status: 'missing',
        installations: [],
        message: '未检测到安装',
        minimumOS: 'Ubuntu 22.04',
      },
      {
        id: 'cc-switch',
        name: 'CC Switch',
        category: 'Management Tools',
        status: 'installed',
        installations: [],
        message: '已安装',
        minimumOS: 'Ubuntu 20.04',
      },
    ],
    ready: 1,
    total: 3,
    needsAttention: 2,
  }
}

let socket
try {
  await waitFor(async () => (await fetch(`http://127.0.0.1:${previewPort}/`)).ok, 'Vite preview')
  const debuggerFile = join(profile, 'DevToolsActivePort')
  const debuggerLines = await waitFor(() => {
    if (!existsSync(debuggerFile)) return null
    const lines = readFileSync(debuggerFile, 'utf8').trim().split('\n')
    return lines.length >= 2 ? lines : null
  }, 'Chrome DevTools')
  const [debuggerPort, debuggerPath] = debuggerLines
  const pages = await waitFor(async () => {
    const value = await (await fetch(`http://127.0.0.1:${debuggerPort}/json/list`)).json()
    const page = value.find((target) => target.type === 'page')
    return page ? [page] : null
  }, 'Chrome page')
  socket = new WebSocket(pages[0].webSocketDebuggerUrl)
  await new Promise((resolve, reject) => {
    socket.addEventListener('open', resolve, { once: true })
    socket.addEventListener('error', reject, { once: true })
  })

  let nextID = 0
  const pending = new Map()
  socket.addEventListener('message', (event) => {
    const message = JSON.parse(event.data)
    if (!message.id || !pending.has(message.id)) return
    const waiter = pending.get(message.id)
    pending.delete(message.id)
    if (message.error) waiter.reject(message.error)
    else waiter.resolve(message.result)
  })
  function send(method, params = {}) {
    const id = ++nextID
    socket.send(JSON.stringify({ id, method, params }))
    return new Promise((resolve, reject) => pending.set(id, { resolve, reject }))
  }

  await send('Page.enable')
  await send('Page.addScriptToEvaluateOnNewDocument', {
    source: `Object.defineProperty(window, 'go', { value: { main: { App: { ScanEnvironment: () => Promise.resolve(${JSON.stringify(scanSnapshot())}) } } }, configurable: true });`,
  })

  for (const width of widths) {
    await send('Emulation.setDeviceMetricsOverride', {
      width,
      height: 900,
      deviceScaleFactor: 1,
      mobile: false,
    })
    const navigation = await send('Page.navigate', {
      url: `http://127.0.0.1:${previewPort}/?responsive-audit=${width}`,
    })
    if (navigation.errorText) {
      throw new Error(`${width}px navigation failed: ${navigation.errorText}`)
    }
    await waitFor(async () => {
      const location = await send('Runtime.evaluate', {
        expression: 'location.href',
        returnByValue: true,
      })
      return location.result.value.includes(`responsive-audit=${width}`)
    }, `${width}px navigation`)
    const evaluated = await send('Runtime.evaluate', {
      expression: `new Promise((resolve) => {
        const deadline = Date.now() + 3000;
        const inspect = () => {
          if (!document.querySelector('.system-card')) {
            if (Date.now() > deadline) resolve({ error: 'dashboard timeout', body: document.body.innerText });
            else setTimeout(inspect, 20);
            return;
          }
          resolve({
            innerWidth,
            scrollWidth: document.documentElement.scrollWidth,
            labels: Array.from(document.querySelectorAll('.sidebar__label')).map((label) => {
              const bounds = label.getBoundingClientRect();
              const style = getComputedStyle(label);
              return { text: label.textContent, width: bounds.width, height: bounds.height, display: style.display, visibility: style.visibility };
            }),
          });
        };
        inspect();
      })`,
      awaitPromise: true,
      returnByValue: true,
    })
    const result = evaluated.result.value
    if (result.error) throw new Error(`${width}px: ${result.error}: ${result.body}`)
    if (result.scrollWidth > result.innerWidth) {
      throw new Error(`${width}px: horizontal overflow ${result.scrollWidth} > ${result.innerWidth}`)
    }
    for (const label of expectedLabels) {
      const evidence = result.labels.find((item) => item.text === label)
      if (
        !evidence ||
        evidence.width <= 1 ||
        evidence.height <= 1 ||
        evidence.display === 'none' ||
        evidence.visibility !== 'visible'
      ) {
        throw new Error(`${width}px: sidebar label ${label} is not visibly laid out`)
      }
    }
    console.log(`${width}px PASS: ${result.scrollWidth} <= ${result.innerWidth}; labels visible`)
  }
} finally {
  socket?.close()
  terminate(preview)
  terminate(chrome)
  await delay(100)
  rmSync(profile, { recursive: true, force: true })
}
