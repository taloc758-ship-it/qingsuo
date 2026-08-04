import { useCallback, useEffect, useState } from 'react'

type Status = {
  running: boolean
  startedAt?: string
  lastExit?: string
  binary: string
  configPath: string
  proxyEndpoint: string
}

type Subscription = {
  configured: boolean
  name?: string
  updatedAt?: string
  nodeCount: number
}

type NodeStatus = {
  tag: string
  name: string
  country: string
  geminiSupport: Availability
  chatgptSupport: Availability
  delayMs: number
  error?: string
}

type Availability = 'supported' | 'unsupported' | 'unknown'
type NodeFilter = 'all' | 'gemini' | 'chatgpt' | 'both' | 'not-supported'
type NodeSort = 'latency-asc' | 'latency-desc' | 'name'

type NodesResponse = {
  running: boolean
  mode: 'auto' | 'manual'
  active: string
  nodes: NodeStatus[]
}

type SystemProxy = {
  supported: boolean
  enabled: boolean
  server?: string
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, init)
  const body = await response.json()
  if (!response.ok) {
    throw new Error(body.error ?? 'Request failed')
  }
  return body as T
}

export default function App() {
  const [status, setStatus] = useState<Status | null>(null)
  const [subscription, setSubscription] = useState<Subscription | null>(null)
  const [subscriptionURL, setSubscriptionURL] = useState('')
  const [nodesState, setNodesState] = useState<NodesResponse | null>(null)
  const [systemProxy, setSystemProxy] = useState<SystemProxy | null>(null)
  const [config, setConfig] = useState('')
  const [logs, setLogs] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [nodeFilter, setNodeFilter] = useState<NodeFilter>('all')
  const [nodeSort, setNodeSort] = useState<NodeSort>('latency-asc')

  const refresh = useCallback(async () => {
    try {
      const [nextStatus, nextConfig, nextLogs, nextSubscription, nextSystemProxy] = await Promise.all([
        request<Status>('/api/status'),
        request<{ content: string }>('/api/config'),
        request<{ content: string }>('/api/logs'),
        request<Subscription>('/api/subscription'),
        request<SystemProxy>('/api/system-proxy'),
      ])
      setStatus(nextStatus)
      setConfig((current) => current || nextConfig.content)
      setLogs(nextLogs.content)
      setSubscription(nextSubscription)
      setSystemProxy(nextSystemProxy)
      if (nextSubscription.configured) {
        const nextNodes = await request<NodesResponse>('/api/nodes')
        setNodesState(nextNodes)
      } else {
        setNodesState(null)
      }
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '无法连接到本地 Agent')
    }
  }, [])

  useEffect(() => {
    void refresh()
    const timer = window.setInterval(() => void refresh(), 3000)
    return () => window.clearInterval(timer)
  }, [refresh])

  async function action(url: string) {
    setBusy(true)
    setMessage('')
    try {
      await request<Status>(url, { method: 'POST' })
      await refresh()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '操作失败')
    } finally {
      setBusy(false)
    }
  }

  async function saveConfig() {
    setBusy(true)
    setMessage('')
    try {
      await request('/api/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: config }),
      })
      setMessage('配置已保存。')
      await refresh()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  async function importSubscription(forceRefresh = false) {
    setBusy(true)
    setMessage('')
    try {
      const result = await request<Subscription>(
        forceRefresh ? '/api/subscription/refresh' : '/api/subscription/import',
        forceRefresh
          ? { method: 'POST' }
          : {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ url: subscriptionURL }),
            },
      )
      setSubscription(result)
      setMessage(`已导入 ${result.nodeCount} 个节点，自动选优组已更新。`)
      await refresh()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '订阅导入失败')
    } finally {
      setBusy(false)
    }
  }

  async function chooseNode(mode: 'auto' | 'manual', tag = '') {
    setBusy(true)
    setMessage('')
    try {
      const result = await request<NodesResponse>('/api/selection', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode, tag }),
      })
      setNodesState(result)
      setMessage(mode === 'auto' ? '已切回自动选优。' : '已切换为手动节点。')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '切换节点失败')
    } finally {
      setBusy(false)
    }
  }

  async function testNodes(tag?: string) {
    setBusy(true)
    setMessage('')
    try {
      await request(tag ? `/api/nodes/${encodeURIComponent(tag)}/test` : '/api/nodes/test', { method: 'POST' })
      setMessage(tag ? '正在测速该节点，请稍候刷新结果。' : '正在并发测试全部节点，请稍候刷新结果。')
      window.setTimeout(() => void refresh(), 1800)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '测速失败')
    } finally {
      setBusy(false)
    }
  }

  async function toggleSystemProxy() {
    const enabled = !systemProxy?.enabled
    setBusy(true)
    setMessage('')
    try {
      const result = await request<SystemProxy>('/api/system-proxy', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled }),
      })
      setSystemProxy(result)
      setMessage(enabled ? 'Windows 系统代理已启用，浏览器流量将通过当前节点。' : 'Windows 系统代理已关闭。')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '更新系统代理失败')
    } finally {
      setBusy(false)
    }
  }

  const running = status?.running ?? false
  const visibleNodes = nodesState?.nodes.filter((node) => {
    if (nodeFilter === 'gemini') return node.geminiSupport === 'supported'
    if (nodeFilter === 'chatgpt') return node.chatgptSupport === 'supported'
    if (nodeFilter === 'both') return node.geminiSupport === 'supported' && node.chatgptSupport === 'supported'
    if (nodeFilter === 'not-supported') return node.geminiSupport !== 'supported' || node.chatgptSupport !== 'supported'
    return true
  }).sort((left, right) => {
    if (nodeSort === 'name') return left.name.localeCompare(right.name, 'zh-CN')

    // Failed and untested nodes have no useful latency, so keep them at the end.
    const leftHasDelay = left.delayMs > 0
    const rightHasDelay = right.delayMs > 0
    if (leftHasDelay !== rightHasDelay) return leftHasDelay ? -1 : 1
    if (!leftHasDelay) return 0
    return nodeSort === 'latency-desc' ? right.delayMs - left.delayMs : left.delayMs - right.delayMs
  })

  function availabilityText(value: Availability) {
    if (value === 'supported') return '支持'
    if (value === 'unsupported') return '不支持'
    return '未识别'
  }

  return (
    <main>
      <header>
        <div>
          <p className="eyebrow">LOCAL SING-BOX CONTROL</p>
          <h1>自动选优代理</h1>
          <p className="muted">导入节点后由 sing-box 的 <code>urltest</code> 出站持续选择可用且低延迟的节点。</p>
        </div>
        <span className={running ? 'badge online' : 'badge offline'}>{running ? '运行中' : '已停止'}</span>
      </header>

      {message && <p className="message">{message}</p>}

      <section className="grid">
        <article>
          <h2>代理状态</h2>
          <dl>
            <div><dt>本地代理</dt><dd>{status?.proxyEndpoint ?? '--'}</dd></div>
            <div><dt>HTTP 代理</dt><dd>http://127.0.0.1:2081</dd></div>
            <div><dt>启动时间</dt><dd>{status?.startedAt ? new Date(status.startedAt).toLocaleString() : '--'}</dd></div>
            <div><dt>上次退出</dt><dd>{status?.lastExit ?? '--'}</dd></div>
          </dl>
          <div className="actions">
            <button disabled={busy || running} onClick={() => void action('/api/start')}>启动</button>
            <button className="secondary" disabled={busy || !running} onClick={() => void action('/api/stop')}>停止</button>
          </div>
        </article>

        <article>
          <h2>运行文件</h2>
          <dl>
            <div><dt>sing-box</dt><dd className="path">{status?.binary ?? '--'}</dd></div>
            <div><dt>配置文件</dt><dd className="path">{status?.configPath ?? '--'}</dd></div>
          </dl>
          <p className="hint">浏览器或系统需设置 HTTP 代理为 `127.0.0.1:2081`，否则网页流量不会经过当前节点。</p>
        </article>
      </section>

      {systemProxy?.supported && (
        <section className="panel system-proxy">
          <div className="panel-title">
            <div>
              <h2>Windows 系统代理</h2>
              <p>{systemProxy.enabled ? `已启用：${systemProxy.server || '127.0.0.1:2081'}。浏览器会使用当前选中的节点。` : '已关闭。浏览器不会经过本地代理。'}</p>
            </div>
            <button className={systemProxy.enabled ? 'secondary' : ''} disabled={busy || (!running && !systemProxy.enabled)} onClick={() => void toggleSystemProxy()}>{systemProxy.enabled ? '关闭系统代理' : '启用系统代理'}</button>
          </div>
        </section>
      )}

      <section className="panel">
        <div className="panel-title">
          <div>
            <h2>订阅节点</h2>
            <p>{subscription?.configured ? `${subscription.name ?? '已导入订阅'}：${subscription.nodeCount} 个节点，最近更新 ${subscription.updatedAt ? new Date(subscription.updatedAt).toLocaleString() : '--'}。` : '粘贴订阅链接。凭据仅保存在本机 Agent 的数据目录。'}</p>
          </div>
          {subscription?.configured && <button className="secondary" disabled={busy} onClick={() => void importSubscription(true)}>刷新订阅</button>}
        </div>
        <div className="subscribe-form">
          <input type="url" value={subscriptionURL} disabled={busy} onChange={(event) => setSubscriptionURL(event.target.value)} placeholder="https://example.com/api/v1/client/subscribe?token=..." />
          <button disabled={busy || !subscriptionURL.trim()} onClick={() => void importSubscription()}>导入订阅</button>
        </div>
      </section>

      {subscription?.configured && (
        <section className="panel">
          <div className="panel-title">
            <div>
              <h2>节点选择与延迟</h2>
              <p>{nodesState?.mode === 'manual' ? `手动模式，正在使用：${nodesState.active || '--'}` : `自动选优，当前生效：${nodesState?.active || '正在探测'}`}。服务标签按节点名称中的地区和内置支持名单判断，不是实际访问测试。</p>
            </div>
            <div className="actions compact">
              <button className="secondary" disabled={busy || !running} onClick={() => void chooseNode('auto')}>自动选择</button>
              <button disabled={busy || !running} onClick={() => void testNodes()}>测试全部延迟</button>
            </div>
          </div>
          <div className="node-filter">
            <label htmlFor="node-filter">筛选节点</label>
            <select id="node-filter" value={nodeFilter} onChange={(event) => setNodeFilter(event.target.value as NodeFilter)}>
              <option value="all">全部节点</option>
              <option value="both">Gemini 和 ChatGPT 都支持</option>
              <option value="gemini">仅 Gemini 支持</option>
              <option value="chatgpt">仅 ChatGPT 支持</option>
              <option value="not-supported">含不支持或未识别</option>
            </select>
            <label htmlFor="node-sort">排序</label>
            <select id="node-sort" value={nodeSort} onChange={(event) => setNodeSort(event.target.value as NodeSort)}>
              <option value="latency-asc">延迟从低到高</option>
              <option value="latency-desc">延迟从高到低</option>
              <option value="name">节点名称</option>
            </select>
            <span>{visibleNodes?.length ?? 0} / {nodesState?.nodes.length ?? 0} 个</span>
          </div>
          <div className="node-list">
            {visibleNodes?.map((node) => (
              <div className={node.tag === nodesState?.active ? 'node active-node' : 'node'} key={node.tag}>
                <div className="node-name">
                  <strong>{node.name}</strong>
                  <span>{node.tag} · {node.country}{node.tag === nodesState?.active ? ' · 当前生效' : ''}</span>
                  <div className="service-support">
                    <span className={`service ${node.geminiSupport}`}>Gemini {availabilityText(node.geminiSupport)}</span>
                    <span className={`service ${node.chatgptSupport}`}>ChatGPT {availabilityText(node.chatgptSupport)}</span>
                  </div>
                </div>
                <span className={node.delayMs > 0 ? 'delay ok' : node.delayMs === -1 ? 'delay bad' : 'delay'}>{node.delayMs > 0 ? `${node.delayMs} ms` : node.delayMs === -1 ? '失败' : '--'}</span>
                <div className="node-actions">
                  <button className="secondary mini" disabled={busy || !running} onClick={() => void testNodes(node.tag)}>测速</button>
                  <button className="mini" disabled={busy || !running} onClick={() => void chooseNode('manual', node.tag)}>使用</button>
                </div>
              </div>
            ))}
          </div>
        </section>
      )}

      <section className="panel">
        <div className="panel-title">
          <div><h2>sing-box 配置</h2><p>{subscription?.configured ? '当前配置由订阅管理，节点凭据不会显示在浏览器页面。' : '默认配置直连且可启动。订阅导入后将自动生成 `urltest` 自动选优组。'}</p></div>
          {!subscription?.configured && <button disabled={busy || running} onClick={() => void saveConfig()}>保存配置</button>}
        </div>
        {subscription?.configured
          ? <p className="hint">已载入 {subscription.nodeCount} 个节点，所有流量会先经 `proxy-auto` 的 `urltest` 自动选优组。</p>
          : <textarea value={config} disabled={running} onChange={(event) => setConfig(event.target.value)} spellCheck="false" />}
      </section>

      <section className="panel">
        <div className="panel-title"><div><h2>核心日志</h2><p>最近的 sing-box 标准输出和错误输出。</p></div><button className="secondary" onClick={() => void refresh()}>刷新</button></div>
        <pre>{logs || '尚无日志。'}</pre>
      </section>
    </main>
  )
}
