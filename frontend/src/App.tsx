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

type RouteRule = {
  id: string
  name: string
  kind: string
  value: string
  outbound: string
  source: string
  editable: boolean
}

type RouteRulesResponse = {
  rules: RouteRule[]
}

type AutoSwitchSettings = {
  failoverOnly: boolean
}

type FailedNodeCleanupSettings = {
  removeFailed: boolean
}

type NodeFilter = 'all' | 'gemini' | 'chatgpt' | 'both' | 'not-supported'
type NodeSort = 'latency-asc' | 'latency-desc' | 'name'

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, init)
  const contentType = response.headers.get('content-type') ?? ''
  const rawBody = await response.text()
  let body: ({ error?: string } & T) | undefined
  try {
    body = contentType.includes('application/json') ? JSON.parse(rawBody) as { error?: string } & T : undefined
  } catch {
    throw new Error(`本地 Agent 返回了无效响应（HTTP ${response.status}），请刷新页面。`)
  }
  if (!response.ok) {
    throw new Error(body?.error ?? (rawBody.trim() || `请求失败（HTTP ${response.status}）`))
  }
  if (!body) {
    throw new Error(`本地 Agent 返回了非 JSON 响应（HTTP ${response.status}），请刷新页面。`)
  }
  return body
}

const Icon = {
  Shield: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 2 4 5v6c0 5 3.5 8 8 11 4.5-3 8-6 8-11V5l-8-3Z" /></svg>),
  Minimize: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M6 12h12" /></svg>),
  Maximize: () => (<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><rect x="6" y="6" width="12" height="12" rx="1" /></svg>),
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
  const [desktopWindow] = useState(() => Boolean(window.qingSuoWindow))
  const [status, setStatus] = useState<Status | null>(null)
  const [subscriptions, setSubscriptions] = useState<SubscriptionSummary[]>([])
  const [subscriptionURL, setSubscriptionURL] = useState('')
  const [nodesState, setNodesState] = useState<NodesResponse | null>(null)
  const [systemProxy, setSystemProxy] = useState<SystemProxy | null>(null)
  const [config, setConfig] = useState('')
  const [logs, setLogs] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [nodeFilter, setNodeFilter] = useState<NodeFilter>('both')
  const [nodeSort, setNodeSort] = useState<NodeSort>('latency-asc')
  const [whitelist, setWhitelist] = useState<string[]>([])
  const [whitelistDomain, setWhitelistDomain] = useState('')
  const [advancedTab, setAdvancedTab] = useState<'config' | 'logs'>('logs')
  const [autoScroll, setAutoScroll] = useState(true)
  const [editingDomain, setEditingDomain] = useState<string | null>(null)
  const [editValue, setEditValue] = useState('')
  const [routeRules, setRouteRules] = useState<RouteRule[]>([])
  const [routeRulesModalOpen, setRouteRulesModalOpen] = useState(false)
  const [routeSearch, setRouteSearch] = useState('')
  const [autoSwitch, setAutoSwitch] = useState<AutoSwitchSettings>({ failoverOnly: false })
  const [failedNodeCleanup, setFailedNodeCleanup] = useState<FailedNodeCleanupSettings>({ removeFailed: false })
  const logRef = useRef<HTMLPreElement>(null)

  const refresh = useCallback(async () => {
    try {
      const [nextStatus, nextConfig, nextLogs, nextSubs, nextSystemProxy, nextWhitelist, nextRouteRules, nextAutoSwitch, nextFailedNodeCleanup] = await Promise.all([
        request<Status>('/api/status'),
        request<{ content: string }>('/api/config'),
        request<{ content: string }>('/api/logs'),
        request<SubscriptionsResponse>('/api/subscriptions'),
        request<SystemProxy>('/api/system-proxy'),
        request<WhitelistResponse>('/api/whitelist'),
        request<RouteRulesResponse>('/api/route-rules'),
        request<AutoSwitchSettings>('/api/auto-switch'),
        request<FailedNodeCleanupSettings>('/api/failed-node-cleanup'),
      ])
      setStatus(nextStatus)
      setConfig((current) => current || nextConfig.content)
      setLogs(nextLogs.content)
      setSubscriptions(nextSubs.groups)
      setSystemProxy(nextSystemProxy)
      setWhitelist(nextWhitelist.domains)
      setRouteRules(nextRouteRules.rules)
      setAutoSwitch(nextAutoSwitch)
      setFailedNodeCleanup(nextFailedNodeCleanup)
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

  async function restartCore() {
    setBusy(true); setMessage('')
    try {
      await request<Status>('/api/restart', { method: 'POST' })
      setMessage('代理核心已重启，系统代理设置保持不变。')
      await refresh()
    }
    catch (error) { setMessage(error instanceof Error ? error.message : '重启失败') }
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

  async function toggleAutoSelection() {
    if (!displayGroup) return
    if (displayGroup.mode !== 'auto' && displayGroup.active) {
      await chooseNode(displayGroup.id, 'auto')
      return
    }
    if (!displayGroup.active) {
      setMessage('当前自动节点仍在探测中，暂时无法固定。')
      return
    }
    await chooseNode(displayGroup.id, 'manual', displayGroup.active)
  }

  async function toggleFailoverOnly() {
    const next = !autoSwitch.failoverOnly
    setBusy(true); setMessage('')
    try {
      const result = await request<AutoSwitchSettings>('/api/auto-switch', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ failoverOnly: next }),
      })
      setAutoSwitch(result)
      setMessage(result.failoverOnly ? '已启用故障才切换：当前节点可用时不会因延迟更低而切换。' : '已启用延迟优选：自动组会优先选择更低延迟节点。')
      await refresh()
    } catch (error) { setMessage(error instanceof Error ? error.message : '更新自动切换设置失败') }
    finally { setBusy(false) }
  }

  async function toggleFailedNodeCleanup() {
    const next = !failedNodeCleanup.removeFailed
    setBusy(true); setMessage('')
    try {
      const result = await request<FailedNodeCleanupSettings>('/api/failed-node-cleanup', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ removeFailed: next }),
      })
      setFailedNodeCleanup(result)
      setMessage(result.removeFailed ? '已启用：测速失败的节点会从当前订阅组删除。' : '已关闭：测速失败的节点只标记为不可用。')
    } catch (error) { setMessage(error instanceof Error ? error.message : '更新失败节点清理设置失败') }
    finally { setBusy(false) }
  }

  async function testNodes(tag?: string, groupId = displayGroup?.id) {
    setBusy(true); setMessage('')
    if (!tag && !groupId) { setMessage('请先选择一个订阅分组。'); setBusy(false); return }
    try { await request(tag ? `/api/nodes/${encodeURIComponent(tag)}/test` : `/api/groups/${encodeURIComponent(groupId!)}/nodes/test`, { method: 'POST' }); setMessage(tag ? '正在测速该节点，请稍候刷新结果。' : '正在并发测试当前分组全部节点，请稍候刷新结果。'); window.setTimeout(() => void refresh(), 1800) }
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
      const result = await request<WhitelistResponse>(`/api/whitelist/${encodeURIComponent(oldDomain)}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ domain: newDomain }) })
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

  const customRouteRules = routeRules.filter((rule) => rule.editable && rule.value.toLowerCase().includes(routeSearch.trim().toLowerCase()))
  const builtInRouteRules = routeRules.filter((rule) => !rule.editable)

  return (
    <main>
      <div className="topbar">
        <div className="brand">
          <div className="brand-mark"><Icon.Shield /></div>
          <span className="brand-name">青梭 QingSuo</span>
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
          <button className="ghost" disabled={busy} onClick={() => void restartCore()}><Icon.Refresh /> 重启</button>
        </div>
        {desktopWindow && (
          <div className="window-controls" aria-label="窗口控制">
            <button className="window-control" title="最小化" aria-label="最小化" onClick={() => void window.qingSuoWindow?.minimize()}><Icon.Minimize /></button>
            <button className="window-control" title="最大化或还原" aria-label="最大化或还原" onClick={() => void window.qingSuoWindow?.toggleMaximize()}><Icon.Maximize /></button>
            <button className="window-control close" title="隐藏到托盘" aria-label="隐藏到托盘" onClick={() => void window.qingSuoWindow?.hide()}><Icon.X /></button>
          </div>
        )}
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
          <button className="route-rules-toggle ghost sm" onClick={() => setRouteRulesModalOpen(true)}>
            <Icon.Layers /> 管理路由规则
            <span className="route-rule-count">{whitelist.length} 条自定义</span>
          </button>
          <div className="route-sidebar-hint">大陆规则自动直连；未匹配的流量默认走代理。</div>
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
      {routeRulesModalOpen && (
        <div className="route-modal-backdrop" role="presentation" onMouseDown={() => { setRouteRulesModalOpen(false); setEditingDomain(null) }}>
          <section className="route-modal" role="dialog" aria-modal="true" aria-label="管理路由规则" onMouseDown={(event) => event.stopPropagation()}>
            <header className="route-modal-head">
              <div>
                <h2>管理路由规则</h2>
                <p>自定义白名单匹配域名和子域名并直连；未匹配流量默认走代理。</p>
              </div>
              <button className="ghost sm" onClick={() => { setRouteRulesModalOpen(false); setEditingDomain(null) }}><Icon.X /></button>
            </header>
            <div className="route-modal-body">
              <section className="route-modal-section">
                <div className="route-modal-section-title">添加直连白名单</div>
                <div className="route-modal-add">
                  <input type="text" value={whitelistDomain} disabled={busy} onChange={(e) => setWhitelistDomain(e.target.value)} placeholder="example.com、*.example.com 或 .vip" onKeyDown={(e) => { if (e.key === 'Enter') void addWhitelist() }} />
                  <button disabled={busy || !whitelistDomain.trim()} onClick={() => void addWhitelist()}><Icon.Plus /> 添加</button>
                </div>
                <p className="route-modal-help">支持域名后缀：填入 <code>.vip</code> 或 <code>*.vip</code>，会匹配所有 <code>*.vip</code> 网站。</p>
              </section>
              <section className="route-modal-section">
                <div className="route-modal-section-title">自定义白名单 <span>{whitelist.length} 条</span></div>
                <input className="route-search" type="search" value={routeSearch} onChange={(e) => setRouteSearch(e.target.value)} placeholder="搜索自定义域名..." />
                <div className="route-rules-list modal-list">
                  {customRouteRules.length > 0 ? customRouteRules.map((rule) => (
                    <div className="route-rule custom" key={rule.id}>
                      <div className="route-rule-main">
                        <span className="route-rule-name">直连域名</span>
                        {editingDomain === rule.value ? (
                          <input className="route-rule-edit-input" value={editValue} disabled={busy} autoFocus onChange={(e) => setEditValue(e.target.value)} onKeyDown={(e) => {
                            if (e.key === 'Enter') void editWhitelist(rule.value, editValue.trim())
                            if (e.key === 'Escape') setEditingDomain(null)
                          }} />
                        ) : <span className="route-rule-value" title={rule.value}>{rule.value}</span>}
                      </div>
                      <div className="route-rule-meta">
                        <span>域名及子域名</span><span className="route-direct">直连</span>
                        {editingDomain === rule.value ? <><button className="route-edit" disabled={busy || !editValue.trim()} onClick={() => void editWhitelist(rule.value, editValue.trim())}>保存</button><button className="route-edit" disabled={busy} onClick={() => setEditingDomain(null)}>取消</button></> : <><button className="route-edit" disabled={busy} onClick={() => { setEditingDomain(rule.value); setEditValue(rule.value) }}>编辑</button><button className="whitelist-delete" disabled={busy} onClick={() => void deleteWhitelist(rule.value)} title={`删除 ${rule.value}`}><Icon.Trash /></button></>}
                      </div>
                    </div>
                  )) : <div className="route-list-empty">{routeSearch ? '没有匹配的自定义规则。' : '还没有自定义白名单。'}</div>}
                </div>
              </section>
              <section className="route-modal-section">
                <div className="route-modal-section-title">内置规则</div>
                <div className="route-rules-list built-in-list">
                  {builtInRouteRules.map((rule) => <div className="route-rule" key={rule.id}><div className="route-rule-main"><span className="route-rule-name">{rule.name}</span><span className="route-rule-value">{rule.value}</span></div><div className="route-rule-meta"><span>{rule.kind}</span><span className={rule.outbound === '直连' ? 'route-direct' : 'route-proxy'}>{rule.outbound}</span><span>内置，不可编辑</span></div></div>)}
                </div>
              </section>
            </div>
          </section>
        </div>
      )}
      <section className="nodes-area">
        <div className="nodes-head">
          <div className="nodes-head-row">
            <div className="nodes-title">
              节点选择
              {displayGroup && <span className="muted">{displayGroup.mode === 'auto' ? '自动选优' : '手动'} · {displayGroup.active || '--'}</span>}
            </div>
            <div className="actions">
              <div className="switch-inline auto-select-switch">
                <span>自动选择</span>
                <div
                  className={`toggle ${displayGroup?.mode === 'auto' ? 'on' : ''}`}
                  role="switch"
                  aria-checked={displayGroup?.mode === 'auto'}
                  aria-label="自动选择节点"
                  onClick={() => !busy && running && void toggleAutoSelection()}
                />
              </div>
              <div className="switch-inline auto-select-switch" title="只在当前自动节点不可用时才换节点">
                <span>故障才切换</span>
                <div
                  className={`toggle ${autoSwitch.failoverOnly ? 'on' : ''}`}
                  role="switch"
                  aria-checked={autoSwitch.failoverOnly}
                  aria-label="故障才切换"
                  onClick={() => !busy && running && void toggleFailoverOnly()}
                />
              </div>
              <div className="switch-inline auto-select-switch" title="测速失败后从订阅分组删除节点">
                <span>失败自动删</span>
                <div
                  className={`toggle ${failedNodeCleanup.removeFailed ? 'on' : ''}`}
                  role="switch"
                  aria-checked={failedNodeCleanup.removeFailed}
                  aria-label="测速失败自动删除节点"
                  onClick={() => !busy && running && void toggleFailedNodeCleanup()}
                />
              </div>
              <button className="sm" disabled={busy || !running || !displayGroup} onClick={() => void testNodes()} title="只测试当前订阅分组"><Icon.Zap /> 测全部</button>
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
