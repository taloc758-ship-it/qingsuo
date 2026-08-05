import { useCallback, useEffect, useRef, useState } from 'react'

type Status = {
  running: boolean
  startedAt?: string
  lastExit?: string
  binary: string
  configPath: string
  proxyEndpoint: string
}

type Availability = 'supported' | 'unsupported' | 'unknown'

type NodeStatus = {
  tag: string
  name: string
  country: string
  geminiSupport: Availability
  chatgptSupport: Availability
  delayMs: number
  error?: string
}

type GroupStatus = {
  id: string
  name: string
  mode: 'auto' | 'manual'
  active: string
  nodes: NodeStatus[]
}

type NodesResponse = {
  running: boolean
  activeGroup: string
  groups: GroupStatus[]
}

type SubscriptionSummary = {
  id: string
  name: string
  url: string
  nodeCount: number
  updatedAt: string
}

type SubscriptionsResponse = {
  groups: SubscriptionSummary[]
}

type SystemProxy = {
  supported: boolean
  enabled: boolean
  server?: string
}

type WhitelistResponse = {
  domains: string[]
}

type NodeFilter = 'all' | 'gemini' | 'chatgpt' | 'both' | 'not-supported'
type NodeSort = 'latency-asc' | 'latency-desc' | 'name'

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, init)
  const body = await response.json()
  if (!response.ok) {
    throw new Error(body.error ?? 'Request failed')
  }
  return body as T
}

const Icon = {
  Shield: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 2 4 5v6c0 5 3.5 8 8 11 4.5-3 8-6 8-11V5l-8-3Z" /></svg>),
  Play: () => (<svg viewBox="0 0 24 24" fill="currentColor"><path d="M8 5v14l11-7z" /></svg>),
  Stop: () => (<svg viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="2" /></svg>),
  Refresh: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M3 12a9 9 0 0 1 15-6.7L21 8" /><path d="M21 3v5h-5" /><path d="M21 12a9 9 0 0 1-15 6.7L3 16" /><path d="M3 21v-5h5" /></svg>),
  Trash: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M3 6h18" /><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" /></svg>),
  Plus: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 5v14M5 12h14" /></svg>),
  Globe: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10" /><path d="M2 12h20M12 2a15 15 0 0 1 0 20 15 15 0 0 1 0-20" /></svg>),
  Layers: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m12 2 9 5-9 5-9-5 9-5Z" /><path d="m3 12 9 5 9-5" /><path d="m3 17 9 5 9-5" /></svg>),
  Zap: () => (<svg viewBox="0 0 24 24" fill="currentColor"><path d="M13 2 3 14h7l-1 8 10-12h-7l1-8z" /></svg>),
  Server: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="2" y="3" width="20" height="8" rx="2" /><rect x="2" y="13" width="20" height="8" rx="2" /><path d="M6 7h.01M6 17h.01" /></svg>),
  Info: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10" /><path d="M12 16v-4M12 8h.01" /></svg>),
  ShieldCheck: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 2 4 5v6c0 5 3.5 8 8 11 4.5-3 8-6 8-11V5l-8-3Z" /><path d="m9 12 2 2 4-4" /></svg>),
  X: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><path d="M18 6 6 18M6 6l12 12" /></svg>),
  FileCode: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" /><path d="M14 2v6h6M10 13l-2 2 2 2M14 13l2 2-2 2" /></svg>),
  Terminal: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m4 17 6-6-6-6M12 19h8" /></svg>),
  Check: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round"><path d="M20 6 9 17l-5-5" /></svg>),
}

export default function App() {
  const [status, setStatus] = useState<Status | null>(null)
  const [subscriptions, setSubscriptions] = useState<SubscriptionSummary[]>([])
  const [subscriptionURL, setSubscriptionURL] = useState('')
  const [nodesState, setNodesState] = useState<NodesResponse | null>(null)
  const [systemProxy, setSystemProxy] = useState<SystemProxy | null>(null)
  const [config, setConfig] = useState('')
  const [logs, setLogs] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [nodeFilter, setNodeFilter] = useState<NodeFilter>('all')
  const [nodeSort, setNodeSort] = useState<NodeSort>('latency-asc')
  const [whitelist, setWhitelist] = useState<string[]>([])
  const [whitelistDomain, setWhitelistDomain] = useState('')
  const [advancedTab, setAdvancedTab] = useState<'config' | 'logs'>('logs')
  const [autoScroll, setAutoScroll] = useState(true)
  const [editingDomain, setEditingDomain] = useState<string | null>(null)
  const [editValue, setEditValue] = useState('')
  const logRef = useRef<HTMLPreElement>(null)

  const refresh = useCallback(async () => {
    try {
      const [nextStatus, nextConfig, nextLogs, nextSubs, nextSystemProxy, nextWhitelist] = await Promise.all([
        request<Status>('/api/status'),
        request<{ content: string }>('/api/config'),
        request<{ content: string }>('/api/logs'),
        request<SubscriptionsResponse>('/api/subscriptions'),
        request<SystemProxy>('/api/system-proxy'),
        request<WhitelistResponse>('/api/whitelist'),
      ])
      setStatus(nextStatus)
      setConfig((current) => current || nextConfig.content)
      setLogs(nextLogs.content)
      setSubscriptions(nextSubs.groups)
      setSystemProxy(nextSystemProxy)
      setWhitelist(nextWhitelist.domains)
      if (nextSubs.groups.length > 0) {
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

  useEffect(() => {
    if (autoScroll && advancedTab === 'logs' && logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight
    }
  }, [logs, autoScroll, advancedTab])

  async function action(url: string) {
    setBusy(true); setMessage('')
    try { await request<Status>(url, { method: 'POST' }); await refresh() }
    catch (error) { setMessage(error instanceof Error ? error.message : '操作失败') }
    finally { setBusy(false) }
  }

  async function saveConfig() {
    setBusy(true); setMessage('')
    try { await request('/api/config', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ content: config }) }); setMessage('配置已保存。'); await refresh() }
    catch (error) { setMessage(error instanceof Error ? error.message : '保存失败') }
    finally { setBusy(false) }
  }

  async function importSubscription() {
    setBusy(true); setMessage('')
    try {
      const result = await request<SubscriptionsResponse>('/api/subscriptions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ url: subscriptionURL }) })
      setSubscriptions(result.groups); setSubscriptionURL(''); setMessage(`已导入订阅，当前共 ${result.groups.length} 个分组。`); await refresh()
    } catch (error) { setMessage(error instanceof Error ? error.message : '订阅导入失败') }
    finally { setBusy(false) }
  }

  async function refreshSubscription(id: string) {
    setBusy(true); setMessage('')
    try { const result = await request<SubscriptionsResponse>(`/api/subscriptions/${encodeURIComponent(id)}/refresh`, { method: 'POST' }); setSubscriptions(result.groups); setMessage('已刷新该订阅分组。'); await refresh() }
    catch (error) { setMessage(error instanceof Error ? error.message : '刷新失败') }
    finally { setBusy(false) }
  }

  async function deleteSubscription(id: string) {
    setBusy(true); setMessage('')
    try { const result = await request<SubscriptionsResponse>(`/api/subscriptions/${encodeURIComponent(id)}`, { method: 'DELETE' }); setSubscriptions(result.groups); setMessage('已删除该订阅分组。'); await refresh() }
    catch (error) { setMessage(error instanceof Error ? error.message : '删除失败') }
    finally { setBusy(false) }
  }

  async function selectGroup(id: string) {
    setBusy(true); setMessage('')
    try { const result = await request<NodesResponse>('/api/selection', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ groupId: id, mode: 'auto' }) }); setNodesState(result); setMessage('已切换到该分组。') }
    catch (error) { setMessage(error instanceof Error ? error.message : '切换分组失败') }
    finally { setBusy(false) }
  }

  async function chooseNode(groupId: string, mode: 'auto' | 'manual', tag = '') {
    setBusy(true); setMessage('')
    try { const result = await request<NodesResponse>('/api/selection', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ groupId, mode, tag }) }); setNodesState(result); setMessage(mode === 'auto' ? '已切回自动选优。' : '已切换为手动节点。') }
    catch (error) { setMessage(error instanceof Error ? error.message : '切换节点失败') }
    finally { setBusy(false) }
  }

  async function autoSelect() {
    if (!displayGroup) return
    if (nodeFilter === 'all') {
      void chooseNode(displayGroup.id, 'auto')
      return
    }
    const candidates = visibleNodes ?? []
    if (candidates.length === 0) {
      setMessage('当前筛选下没有可用节点。')
      return
    }
    const tested = candidates.filter((n) => n.delayMs > 0).sort((a, b) => a.delayMs - b.delayMs)
    if (tested.length > 0) {
      setBusy(true); setMessage('')
      try {
        const result = await request<NodesResponse>('/api/selection', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ groupId: displayGroup.id, mode: 'manual', tag: tested[0].tag }) })
        setNodesState(result)
        setMessage(`已根据筛选选择最优节点：${tested[0].name}（${tested[0].delayMs}ms）`)
      } catch (error) { setMessage(error instanceof Error ? error.message : '切换失败') }
      finally { setBusy(false) }
    } else {
      setMessage('正在测速，完成后请再次点击自动选择。')
      void testNodes()
    }
  }

  async function testNodes(tag?: string) {
    setBusy(true); setMessage('')
    try { await request(tag ? `/api/nodes/${encodeURIComponent(tag)}/test` : '/api/nodes/test', { method: 'POST' }); setMessage(tag ? '正在测速该节点，请稍候刷新结果。' : '正在并发测试全部节点，请稍候刷新结果。'); window.setTimeout(() => void refresh(), 1800) }
    catch (error) { setMessage(error instanceof Error ? error.message : '测速失败') }
    finally { setBusy(false) }
  }

  async function toggleSystemProxy() {
    const enabled = !systemProxy?.enabled
    setBusy(true); setMessage('')
    try { const result = await request<SystemProxy>('/api/system-proxy', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled }) }); setSystemProxy(result); setMessage(enabled ? 'Windows 系统代理已启用，浏览器流量将通过当前节点。' : 'Windows 系统代理已关闭。') }
    catch (error) { setMessage(error instanceof Error ? error.message : '更新系统代理失败') }
    finally { setBusy(false) }
  }

  async function addWhitelist() {
    const domain = whitelistDomain.trim()
    if (!domain) return
    setBusy(true); setMessage('')
    try {
      const result = await request<WhitelistResponse>('/api/whitelist', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ domain }) })
      setWhitelist(result.domains); setWhitelistDomain(''); setMessage(`已添加 ${domain} 到白名单，该域名及子域名将走直连。`); await refresh()
    } catch (error) { setMessage(error instanceof Error ? error.message : '添加白名单失败') }
    finally { setBusy(false) }
  }

  async function deleteWhitelist(domain: string) {
    setBusy(true); setMessage('')
    try {
      const result = await request<WhitelistResponse>(`/api/whitelist/${encodeURIComponent(domain)}`, { method: 'DELETE' })
      setWhitelist(result.domains); setMessage(`已从白名单移除 ${domain}。`); await refresh()
    } catch (error) { setMessage(error instanceof Error ? error.message : '删除白名单失败') }
    finally { setBusy(false) }
  }

  async function editWhitelist(oldDomain: string, newDomain: string) {
    setEditingDomain(null)
    if (!newDomain || oldDomain === newDomain) return
    setBusy(true); setMessage('')
    try {
      await request(`/api/whitelist/${encodeURIComponent(oldDomain)}`, { method: 'DELETE' })
      const result = await request<WhitelistResponse>('/api/whitelist', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ domain: newDomain }) })
      setWhitelist(result.domains); setMessage(`已更新白名单：${oldDomain} → ${newDomain}`); await refresh()
    } catch (error) { setMessage(error instanceof Error ? error.message : '更新白名单失败') }
    finally { setBusy(false) }
  }

  const running = status?.running ?? false
  const displayGroup = nodesState?.groups.find((g) => g.id === nodesState.activeGroup) ?? nodesState?.groups[0] ?? null
  const hasSubscriptions = subscriptions.length > 0
  const activeNodeName = displayGroup?.nodes.find((n) => n.tag === displayGroup.active)?.name ?? displayGroup?.active ?? '--'

  const visibleNodes = displayGroup?.nodes.filter((node) => {
    if (nodeFilter === 'gemini') return node.geminiSupport === 'supported'
    if (nodeFilter === 'chatgpt') return node.chatgptSupport === 'supported'
    if (nodeFilter === 'both') return node.geminiSupport === 'supported' && node.chatgptSupport === 'supported'
    if (nodeFilter === 'not-supported') return node.geminiSupport !== 'supported' || node.chatgptSupport !== 'supported'
    return true
  }).sort((left, right) => {
    if (nodeSort === 'name') return left.name.localeCompare(right.name, 'zh-CN')
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

  function delayBar(ms: number) {
    if (ms <= 0) return 0
    const pct = Math.max(8, Math.min(100, 100 - (ms - 50) / 8))
    return pct
  }

  return (
    <main>
      <div className="topbar">
        <div className="brand">
          <div className="brand-mark"><Icon.Shield /></div>
          <span className="brand-name">Sing-Box</span>
        </div>
        <div className="topbar-sep" />
        <span className={running ? 'pill on' : 'pill off'}>
          <span className="dot" />
          {running ? '运行中' : '已停止'}
        </span>
        <div className="topbar-info">
          <span className="mono">127.0.0.1:2081</span>
          <span className="topbar-sep" />
          <span>节点 <b>{activeNodeName}</b></span>
        </div>
        <div className="topbar-spacer" />
        <div className="topbar-actions">
          <button disabled={busy || running} onClick={() => void action('/api/start')}><Icon.Play /> 启动</button>
          <button className="ghost" disabled={busy || !running} onClick={() => void action('/api/stop')}><Icon.Stop /></button>
        </div>
      </div>

      {message && <div className="toast"><Icon.Info /> {message}</div>}

      <aside className="sidebar">
        <div className="sb-section">
          <div className="sb-label">订阅分组</div>
          <div className="sub-form">
            <input type="url" value={subscriptionURL} disabled={busy} onChange={(e) => setSubscriptionURL(e.target.value)} placeholder="订阅链接..." />
            <button className="ghost sm" disabled={busy || !subscriptionURL.trim()} onClick={() => void importSubscription()}><Icon.Plus /></button>
          </div>
          <div className="sub-list">
            {subscriptions.map((sub) => (
              <div className="sub-row" key={sub.id}>
                <div className="info">
                  <div className="ic"><Icon.Layers /></div>
                  <div className="meta">
                    <div className="nm">{sub.name}</div>
                    <div className="sub">{sub.nodeCount} 节点 · {sub.updatedAt ? new Date(sub.updatedAt).toLocaleDateString() : '--'}</div>
                  </div>
                </div>
                <div className="btns">
                  <button className="ghost sm" disabled={busy} onClick={() => void refreshSubscription(sub.id)}><Icon.Refresh /></button>
                  <button className="danger sm" disabled={busy} onClick={() => void deleteSubscription(sub.id)}><Icon.Trash /></button>
                </div>
              </div>
            ))}
          </div>
        </div>

        <div className="sb-section">
          <div className="sb-label">路由规则</div>
          <div className="wl-form">
            <input type="text" value={whitelistDomain} disabled={busy} onChange={(e) => setWhitelistDomain(e.target.value)} placeholder="白名单域名..." onKeyDown={(e) => { if (e.key === 'Enter') void addWhitelist() }} />
            <button className="ghost sm" disabled={busy || !whitelistDomain.trim()} onClick={() => void addWhitelist()}>添加</button>
          </div>
          {whitelist.length > 0 ? (
            <>
            <div className="wl-chips">
              {whitelist.map((d) => (
                <span className="chip" key={d}>
                  {editingDomain === d ? (
                    <input
                      className="chip-edit"
                      value={editValue}
                      disabled={busy}
                      autoFocus
                      onChange={(e) => setEditValue(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') void editWhitelist(d, editValue.trim())
                        if (e.key === 'Escape') setEditingDomain(null)
                      }}
                      onBlur={() => setEditingDomain(null)}
                    />
                  ) : (
                    <span className="chip-text" onClick={() => { setEditingDomain(d); setEditValue(d) }}>{d}</span>
                  )}
                  <button className="x" disabled={busy} onClick={() => void deleteWhitelist(d)}><Icon.X /></button>
                </span>
              ))}
            </div>
            <div className="wl-hint">点击域名可编辑</div>
            </>
          ) : (
            <div className="wl-empty">国内域名已自动直连 · 添加自定义域名走直连</div>
          )}
          {systemProxy?.supported && (
            <div className="sys-proxy" style={{ marginTop: '8px' }}>
              <div>
                <div className="nm">系统代理</div>
                <div className="st">{systemProxy.enabled ? '已开启' : '已关闭'}</div>
              </div>
              <div className={systemProxy.enabled ? 'toggle on' : 'toggle'} role="button" onClick={() => !busy && (running || systemProxy.enabled) && void toggleSystemProxy()} />
            </div>
          )}
        </div>
      </aside>
      <section className="nodes-area">
        <div className="nodes-head">
          <div className="nodes-head-row">
            <div className="nodes-title">
              节点选择
              {displayGroup && <span className="muted">{displayGroup.mode === 'auto' ? '自动选优' : '手动'} · {displayGroup.active || '--'}</span>}
            </div>
            <div className="actions">
              <button className="ghost sm" disabled={busy || !running} onClick={() => void autoSelect()}><Icon.Check /> 自动</button>
              <button className="sm" disabled={busy || !running} onClick={() => void testNodes()}><Icon.Zap /> 测全部</button>
            </div>
          </div>
          {hasSubscriptions && nodesState && (
            <>
              <div className="group-tabs">
                {nodesState.groups.map((g) => (
                  <button key={g.id} className={g.id === (displayGroup?.id ?? '') ? 'gtab active' : 'gtab'} disabled={busy || !running || g.id === displayGroup?.id} onClick={() => void selectGroup(g.id)}>
                    {g.name}<span className="cnt">({g.nodes.length})</span>
                  </button>
                ))}
              </div>
              <div className="filter-bar">
                <label>筛选</label>
                <select value={nodeFilter} onChange={(e) => setNodeFilter(e.target.value as NodeFilter)}>
                  <option value="all">全部</option>
                  <option value="both">Gemini+ChatGPT</option>
                  <option value="gemini">Gemini</option>
                  <option value="chatgpt">ChatGPT</option>
                  <option value="not-supported">不支持</option>
                </select>
                <label>排序</label>
                <select value={nodeSort} onChange={(e) => setNodeSort(e.target.value as NodeSort)}>
                  <option value="latency-asc">延迟↑</option>
                  <option value="latency-desc">延迟↓</option>
                  <option value="name">名称</option>
                </select>
                <span className="count"><b>{visibleNodes?.length ?? 0}</b>/{displayGroup?.nodes.length ?? 0}</span>
              </div>
            </>
          )}
        </div>
        <div className="nlist">
          {hasSubscriptions && visibleNodes && visibleNodes.length > 0 ? (
            visibleNodes.map((node) => {
              const dp = node.delayMs > 0 ? 'ok' : node.delayMs === -1 ? 'bad' : 'idle'
              return (
                <div className={node.tag === displayGroup?.active ? 'nrow on' : 'nrow'} key={node.tag}>
                  <span className="nm">{node.name}{node.tag === displayGroup?.active && <span className="badge">活跃</span>}</span>
                  <span className="ctry">{node.country}</span>
                  <span className="svcs">
                    <span className={`svc ${node.geminiSupport === 'supported' ? 'yes' : node.geminiSupport === 'unsupported' ? 'no' : 'unk'}`}>Gemini {node.geminiSupport === 'supported' ? '✓' : node.geminiSupport === 'unsupported' ? '✗' : '?'}</span>
                    <span className={`svc ${node.chatgptSupport === 'supported' ? 'yes' : node.chatgptSupport === 'unsupported' ? 'no' : 'unk'}`}>ChatGPT {node.chatgptSupport === 'supported' ? '✓' : node.chatgptSupport === 'unsupported' ? '✗' : '?'}</span>
                  </span>
                  <span className={`lat ${dp}`}>{node.delayMs > 0 ? `${node.delayMs}ms` : node.delayMs === -1 ? '×' : '--'}</span>
                  <span className="btns">
                    <button className="ghost sm" disabled={busy || !running} onClick={() => void testNodes(node.tag)}>测速</button>
                    <button className="sm" disabled={busy || !running} onClick={() => displayGroup && void chooseNode(displayGroup.id, 'manual', node.tag)}>使用</button>
                  </span>
                </div>
              )
            })
          ) : (
            <div className="nlist-empty">{hasSubscriptions ? '没有匹配的节点' : '请在左侧导入订阅链接'}</div>
          )}
        </div>
      </section>

      <section className="bottom-area">
        <div className="bottom-head">
          <div className="tab-bar">
            <button className={`tab-btn ${advancedTab === 'logs' ? 'active' : ''}`} onClick={() => setAdvancedTab('logs')}>日志</button>
            <button className={`tab-btn ${advancedTab === 'config' ? 'active' : ''}`} onClick={() => setAdvancedTab('config')}>配置</button>
          </div>
          <div className="tab-extra">
            {advancedTab === 'config' && !hasSubscriptions && <button className="ghost sm" disabled={busy || running} onClick={() => void saveConfig()}>保存</button>}
            {advancedTab === 'logs' && (
              <>
                <div className="switch-inline">
                  <span>自动滚动</span>
                  <div className={`toggle ${autoScroll ? 'on' : ''}`} role="button" onClick={() => setAutoScroll((v) => !v)} />
                </div>
                <button className="ghost sm" onClick={() => void refresh()}><Icon.Refresh /></button>
              </>
            )}
          </div>
        </div>
        <div className="bottom-body">
          {advancedTab === 'config' ? (
            hasSubscriptions
              ? <div className="hint"><Icon.FileCode /> 配置由订阅分组自动管理 · <code>proxy</code> 选择器 + <code>urltest</code> · 国内走白名单直连</div>
              : <textarea value={config} disabled={running} onChange={(e) => setConfig(e.target.value)} spellCheck="false" />
          ) : (
            <pre ref={logRef}>{logs || '尚无日志。'}</pre>
          )}
        </div>
      </section>
    </main>
  )
}