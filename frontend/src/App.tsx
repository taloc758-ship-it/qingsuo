import { useCallback, useEffect, useRef, useState } from 'react'
import orbitPreview from '../../assets/icons/qingsuo-orbit.png'
import shieldPreview from '../../assets/icons/qingsuo-shield.png'
import prismPreview from '../../assets/icons/qingsuo-prism.png'
import pulsePreview from '../../assets/icons/qingsuo-pulse.png'
import knotPreview from '../../assets/icons/qingsuo-knot.png'

type Status = {
  running: boolean
  startedAt?: string
  lastExit?: string
  binary: string
  configPath: string
  proxyEndpoint: string
}

type NodeStatus = {
  tag: string
  name: string
  country: string
  delayMs: number
  googleDelayMs: number
  geminiDelayMs: number
  chatgptDelayMs: number
  error?: string
}

type GroupStatus = {
  id: string
  name: string
  auto: boolean
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

type RoutingMode = {
	globalProxy: boolean
}

type TunMode = {
	supported: boolean
	enabled: boolean
	configured: boolean
	elevated: boolean
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
  autoSelection: boolean
  failoverOnly: boolean
  switchInterval: SwitchInterval
}

type SwitchInterval = '30s' | '1m' | '3m' | '5m' | '10m' | '30m'
type ThemeName = 'carbon' | 'ocean' | 'paper' | 'contrast'
type IconVariant = AppIconSettings['selected']

const themes: Array<{ value: ThemeName, label: string }> = [
  { value: 'carbon', label: '碳黑绿' },
  { value: 'ocean', label: '深海蓝' },
  { value: 'paper', label: '纸白红' },
  { value: 'contrast', label: '高对比' },
]

const switchIntervals: Array<{ value: SwitchInterval, label: string }> = [
  { value: '30s', label: '30 秒' },
  { value: '1m', label: '1 分钟' },
  { value: '3m', label: '3 分钟' },
  { value: '5m', label: '5 分钟' },
  { value: '10m', label: '10 分钟' },
  { value: '30m', label: '30 分钟' },
]

const iconVariants: Array<{ value: IconVariant, label: string, description: string }> = [
  { value: 'orbit', label: '轨道', description: '环绕连接' },
  { value: 'shield', label: '盾牌', description: '安全守护' },
  { value: 'prism', label: '棱镜', description: '多彩折射' },
  { value: 'pulse', label: '脉冲', description: '网络跃动' },
  { value: 'knot', label: '环结', description: '稳定连接' },
]

const iconPreviewSources: Record<IconVariant, string> = {
  orbit: orbitPreview,
  shield: shieldPreview,
  prism: prismPreview,
  pulse: pulsePreview,
  knot: knotPreview,
}

type FailedNodeCleanupSettings = {
  removeFailed: boolean
}

type NodeSort = 'combined-asc' | 'combined-desc' | 'google-asc' | 'gemini-asc' | 'chatgpt-asc' | 'name'
type TestService = 'all' | 'google' | 'gemini' | 'chatgpt'

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
  const [routingMode, setRoutingMode] = useState<RoutingMode>({ globalProxy: false })
	const [tunMode, setTunMode] = useState<TunMode | null>(null)
  const [autoLaunch, setAutoLaunch] = useState<AutoLaunchSettings | null>(null)
  const [iconVariant, setIconVariant] = useState<IconVariant>('shield')
  const [config, setConfig] = useState('')
  const [logs, setLogs] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [testService, setTestService] = useState<TestService>('all')
  const [nodeSort, setNodeSort] = useState<NodeSort>('combined-asc')
  const [whitelist, setWhitelist] = useState<string[]>([])
  const [whitelistDomain, setWhitelistDomain] = useState('')
  const [advancedTab, setAdvancedTab] = useState<'config' | 'logs'>('logs')
  const [autoScroll, setAutoScroll] = useState(true)
  const [editingDomain, setEditingDomain] = useState<string | null>(null)
  const [editValue, setEditValue] = useState('')
  const [routeRules, setRouteRules] = useState<RouteRule[]>([])
  const [routeRulesModalOpen, setRouteRulesModalOpen] = useState(false)
  const [routeSearch, setRouteSearch] = useState('')
  const [autoSwitch, setAutoSwitch] = useState<AutoSwitchSettings>({ autoSelection: true, failoverOnly: false, switchInterval: '5m' })
  const [theme, setTheme] = useState<ThemeName>(() => {
    const saved = window.localStorage.getItem('qingsuo-theme')
    return themes.some((candidate) => candidate.value === saved) ? saved as ThemeName : 'carbon'
  })
  const [failedNodeCleanup, setFailedNodeCleanup] = useState<FailedNodeCleanupSettings>({ removeFailed: false })
  const logRef = useRef<HTMLPreElement>(null)
  const toastTimerRef = useRef<number | null>(null)

  const notify = useCallback((nextMessage: string) => {
    if (toastTimerRef.current !== null) window.clearTimeout(toastTimerRef.current)
    setMessage(nextMessage)
    if (nextMessage) {
      toastTimerRef.current = window.setTimeout(() => {
        setMessage('')
        toastTimerRef.current = null
      }, 4200)
    }
  }, [])

  const refresh = useCallback(async () => {
    try {
      const [nextStatus, nextConfig, nextLogs, nextSubs, nextSystemProxy, nextRoutingMode, nextTunMode, nextWhitelist, nextRouteRules, nextAutoSwitch, nextFailedNodeCleanup] = await Promise.all([
        request<Status>('/api/status'),
        request<{ content: string }>('/api/config'),
        request<{ content: string }>('/api/logs'),
        request<SubscriptionsResponse>('/api/subscriptions'),
        request<SystemProxy>('/api/system-proxy'),
        request<RoutingMode>('/api/routing-mode'),
			request<TunMode>('/api/tun-mode'),
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
      setRoutingMode(nextRoutingMode)
			setTunMode(nextTunMode)
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
      notify(error instanceof Error ? error.message : '无法连接到本地 Agent')
    }
  }, [])

  useEffect(() => {
    void refresh()
    const timer = window.setInterval(() => void refresh(), 3000)
    return () => window.clearInterval(timer)
  }, [refresh])

  useEffect(() => {
    if (!desktopWindow) return
    let active = true
    void window.qingSuoWindow?.getAutoLaunchSettings()
      .then((settings) => { if (active) setAutoLaunch(settings) })
      .catch(() => { if (active) notify('无法读取开机自启动设置。') })
    return () => { active = false }
  }, [desktopWindow, notify])

  useEffect(() => {
    if (!desktopWindow) return
    let active = true
    void window.qingSuoWindow?.getIconSettings()
      .then((settings) => { if (active) setIconVariant(settings.selected) })
      .catch(() => { if (active) notify('无法读取图标设置。') })
    return () => { active = false }
  }, [desktopWindow, notify])

  useEffect(() => () => {
    if (toastTimerRef.current !== null) window.clearTimeout(toastTimerRef.current)
  }, [])

  useEffect(() => {
    if (autoScroll && advancedTab === 'logs' && logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight
    }
  }, [logs, autoScroll, advancedTab])

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    window.localStorage.setItem('qingsuo-theme', theme)
  }, [theme])

  async function restartCore() {
    setBusy(true); notify('')
    try {
      await request<Status>('/api/restart', { method: 'POST' })
      notify('代理核心已重启，系统代理设置保持不变。')
      await refresh()
    }
    catch (error) { notify(error instanceof Error ? error.message : '重启失败') }
    finally { setBusy(false) }
  }

  async function saveConfig() {
    setBusy(true); notify('')
    try { await request('/api/config', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ content: config }) }); notify('配置已保存。'); await refresh() }
    catch (error) { notify(error instanceof Error ? error.message : '保存失败') }
    finally { setBusy(false) }
  }

  async function importSubscription() {
    setBusy(true); notify('')
    try {
      const result = await request<SubscriptionsResponse>('/api/subscriptions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ url: subscriptionURL }) })
      setSubscriptions(result.groups); setSubscriptionURL(''); notify(`已导入订阅，当前共 ${result.groups.length} 个分组。`); await refresh()
    } catch (error) { notify(error instanceof Error ? error.message : '订阅导入失败') }
    finally { setBusy(false) }
  }

  async function refreshSubscription(id: string) {
    setBusy(true); notify('')
    try { const result = await request<SubscriptionsResponse>(`/api/subscriptions/${encodeURIComponent(id)}/refresh`, { method: 'POST' }); setSubscriptions(result.groups); notify('已刷新该订阅分组。'); await refresh() }
    catch (error) { notify(error instanceof Error ? error.message : '刷新失败') }
    finally { setBusy(false) }
  }

  async function deleteSubscription(id: string) {
    setBusy(true); notify('')
    try { const result = await request<SubscriptionsResponse>(`/api/subscriptions/${encodeURIComponent(id)}`, { method: 'DELETE' }); setSubscriptions(result.groups); notify('已删除该订阅分组。'); await refresh() }
    catch (error) { notify(error instanceof Error ? error.message : '删除失败') }
    finally { setBusy(false) }
  }

  async function selectGroup(id: string) {
    setBusy(true); notify('')
    try { const result = await request<NodesResponse>('/api/selection', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ groupId: id, mode: 'auto' }) }); setNodesState(result); notify('已切换到该分组。') }
    catch (error) { notify(error instanceof Error ? error.message : '切换分组失败') }
    finally { setBusy(false) }
  }

  async function chooseNode(groupId: string, mode: 'auto' | 'node', tag = '') {
    setBusy(true); notify('')
    try { const result = await request<NodesResponse>('/api/selection', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ groupId, mode, tag }) }); setNodesState(result); notify(mode === 'auto' ? '已切换到自动选优。' : '已使用此节点；自动选择状态不变。') }
    catch (error) { notify(error instanceof Error ? error.message : '切换节点失败') }
    finally { setBusy(false) }
  }

  async function toggleAutoSelection() {
    if (!displayGroup) return
    const next = !autoSwitch.autoSelection
    setBusy(true); notify('')
    try {
      const result = await request<AutoSwitchSettings>('/api/auto-switch', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ failoverOnly: autoSwitch.failoverOnly, autoSelection: next }),
      })
      setAutoSwitch(result)
      notify(result.autoSelection ? '已开启自动选择。' : '已关闭自动选择，当前节点将保持不变。')
      await refresh()
    } catch (error) { notify(error instanceof Error ? error.message : '更新自动选择失败') }
    finally { setBusy(false) }
  }

  async function toggleFailoverOnly() {
    const next = !autoSwitch.failoverOnly
    setBusy(true); notify('')
    try {
      const result = await request<AutoSwitchSettings>('/api/auto-switch', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ failoverOnly: next, autoSelection: autoSwitch.autoSelection }),
      })
      setAutoSwitch(result)
      notify(result.failoverOnly ? '已启用故障才切换：当前节点可用时不会因延迟更低而切换。' : '已启用延迟优选：自动组会优先选择更低延迟节点。')
      await refresh()
    } catch (error) { notify(error instanceof Error ? error.message : '更新自动切换设置失败') }
    finally { setBusy(false) }
  }

  async function changeSwitchInterval(switchInterval: SwitchInterval) {
    setBusy(true); notify('')
    try {
      const result = await request<AutoSwitchSettings>('/api/auto-switch', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ switchInterval }),
      })
      setAutoSwitch(result)
      const label = switchIntervals.find((item) => item.value === result.switchInterval)?.label ?? result.switchInterval
      notify(`自动选优周期已设为 ${label}。`)
    } catch (error) { notify(error instanceof Error ? error.message : '更新自动切换周期失败') }
    finally { setBusy(false) }
  }

  async function toggleFailedNodeCleanup() {
    const next = !failedNodeCleanup.removeFailed
    setBusy(true); notify('')
    try {
      const result = await request<FailedNodeCleanupSettings>('/api/failed-node-cleanup', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ removeFailed: next }),
      })
      setFailedNodeCleanup(result)
      notify(result.removeFailed ? '已启用：测速失败的节点会从当前订阅组删除。' : '已关闭：测速失败的节点只标记为不可用。')
    } catch (error) { notify(error instanceof Error ? error.message : '更新失败节点清理设置失败') }
    finally { setBusy(false) }
  }

  async function testNodes(tag?: string, groupId = displayGroup?.id) {
    setBusy(true); notify('')
    if (!tag && !groupId) { notify('请先选择一个订阅分组。'); setBusy(false); return }
    const label = testService === 'all' ? 'Google、Gemini、ChatGPT' : testService === 'google' ? 'Google' : testService === 'gemini' ? 'Gemini' : 'ChatGPT'
    const endpoint = tag ? `/api/nodes/${encodeURIComponent(tag)}/test` : `/api/groups/${encodeURIComponent(groupId!)}/nodes/test`
    try { await request(`${endpoint}?service=${testService}`, { method: 'POST' }); notify(tag ? `正在测试 ${label}，请稍候刷新结果。` : `正在并发测试当前分组的 ${label}，请稍候刷新结果。`); window.setTimeout(() => void refresh(), 1800) }
    catch (error) { notify(error instanceof Error ? error.message : '测速失败') }
    finally { setBusy(false) }
  }

  async function toggleSystemProxy() {
    const enabled = !systemProxy?.enabled
    setBusy(true); notify('')
    try { const result = await request<SystemProxy>('/api/system-proxy', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled }) }); setSystemProxy(result); notify(enabled ? 'Windows 系统代理已启用，浏览器流量将通过当前节点。' : 'Windows 系统代理已关闭。') }
    catch (error) { notify(error instanceof Error ? error.message : '更新系统代理失败') }
    finally { setBusy(false) }
  }

  async function toggleGlobalProxy() {
    const globalProxy = !routingMode.globalProxy
    setBusy(true); notify('')
    try {
      const result = await request<RoutingMode>('/api/routing-mode', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ globalProxy }),
      })
      setRoutingMode(result)
      notify(result.globalProxy ? '已开启全局代理：所有流量均通过当前代理节点。' : '已恢复规则分流：大陆和自定义白名单直连。')
      await refresh()
    } catch (error) { notify(error instanceof Error ? error.message : '更新全局代理模式失败') }
    finally { setBusy(false) }
  }

  async function toggleTunMode() {
    if (!tunMode?.supported) return
		const enabled = !tunMode.configured
    setBusy(true); notify('')
    try {
      const result = await request<TunMode>('/api/tun-mode', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled }),
      })
      setTunMode(result)
      notify(result.enabled ? 'TUN 模式已开启：不遵守系统代理的软件也会接入青梭。' : 'TUN 模式已关闭，虚拟网卡和路由已恢复。')
      await refresh()
    } catch (error) { notify(error instanceof Error ? error.message : '更新 TUN 模式失败') }
    finally { setBusy(false) }
  }

  async function toggleAutoLaunch() {
    if (!autoLaunch?.supported || !window.qingSuoWindow) return
    const enabled = !autoLaunch.enabled
    setBusy(true); notify('')
    try {
      const result = await window.qingSuoWindow.setAutoLaunchEnabled(enabled)
      setAutoLaunch(result)
      notify(enabled ? '已开启开机自启动，登录 Windows 后将最小化到托盘。' : '已关闭开机自启动。')
    } catch (error) { notify(error instanceof Error ? error.message : '更新开机自启动设置失败') }
    finally { setBusy(false) }
  }

  async function changeIconVariant(nextIconVariant: IconVariant) {
    if (!window.qingSuoWindow) return
    setBusy(true); notify('')
    try {
      const settings = await window.qingSuoWindow.setIconVariant(nextIconVariant)
      setIconVariant(settings.selected)
      const label = iconVariants.find((icon) => icon.value === settings.selected)?.label ?? settings.selected
      notify(`已切换为${label}图标，任务栏和托盘已同步更新。`)
    } catch (error) { notify(error instanceof Error ? error.message : '切换图标失败') }
    finally { setBusy(false) }
  }

  async function addWhitelist() {
    const domain = whitelistDomain.trim()
    if (!domain) return
    setBusy(true); notify('')
    try {
      const result = await request<WhitelistResponse>('/api/whitelist', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ domain }) })
      setWhitelist(result.domains); setWhitelistDomain(''); notify(`已添加 ${domain} 到白名单，该域名及子域名将走直连。`); await refresh()
    } catch (error) { notify(error instanceof Error ? error.message : '添加白名单失败') }
    finally { setBusy(false) }
  }

  async function deleteWhitelist(domain: string) {
    setBusy(true); notify('')
    try {
      const result = await request<WhitelistResponse>(`/api/whitelist/${encodeURIComponent(domain)}`, { method: 'DELETE' })
      setWhitelist(result.domains); notify(`已从白名单移除 ${domain}。`); await refresh()
    } catch (error) { notify(error instanceof Error ? error.message : '删除白名单失败') }
    finally { setBusy(false) }
  }

  async function editWhitelist(oldDomain: string, newDomain: string) {
    setEditingDomain(null)
    if (!newDomain || oldDomain === newDomain) return
    setBusy(true); notify('')
    try {
      const result = await request<WhitelistResponse>(`/api/whitelist/${encodeURIComponent(oldDomain)}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ domain: newDomain }) })
      setWhitelist(result.domains); notify(`已更新白名单：${oldDomain} → ${newDomain}`); await refresh()
    } catch (error) { notify(error instanceof Error ? error.message : '更新白名单失败') }
    finally { setBusy(false) }
  }

  const running = status?.running ?? false
  const displayGroup = nodesState?.groups.find((g) => g.id === nodesState.activeGroup) ?? nodesState?.groups[0] ?? null
  const hasSubscriptions = subscriptions.length > 0
  const activeNodeName = displayGroup?.nodes.find((n) => n.tag === displayGroup.active)?.name ?? displayGroup?.active ?? '--'

  const visibleNodes = displayGroup?.nodes.slice().sort((left, right) => {
    if (nodeSort === 'name') return left.name.localeCompare(right.name, 'zh-CN')
    const delayField: keyof Pick<NodeStatus, 'delayMs' | 'googleDelayMs' | 'geminiDelayMs' | 'chatgptDelayMs'> =
      nodeSort === 'google-asc' ? 'googleDelayMs'
        : nodeSort === 'gemini-asc' ? 'geminiDelayMs'
          : nodeSort === 'chatgpt-asc' ? 'chatgptDelayMs'
            : 'delayMs'
    const leftDelay = left[delayField]
    const rightDelay = right[delayField]
    const leftHasDelay = leftDelay > 0
    const rightHasDelay = rightDelay > 0
    if (leftHasDelay !== rightHasDelay) return leftHasDelay ? -1 : 1
    if (!leftHasDelay) return 0
    return nodeSort === 'combined-desc' ? rightDelay - leftDelay : leftDelay - rightDelay
  })

  function formatDelay(ms: number) {
    return ms > 0 ? `${ms}ms` : ms === -1 ? '×' : '--'
  }

  const customRouteRules = routeRules.filter((rule) => rule.editable && rule.value.toLowerCase().includes(routeSearch.trim().toLowerCase()))
  const builtInRouteRules = routeRules.filter((rule) => !rule.editable)

  return (
    <main>
      <div className="topbar">
        <div className="brand">
          <div className="brand-mark"><img src={iconPreviewSources[iconVariant]} alt="" /></div>
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
          <label className="theme-picker" title="切换界面主题">
            <span>主题</span>
            <select value={theme} onChange={(event) => setTheme(event.target.value as ThemeName)}>
              {themes.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
            </select>
          </label>
          {desktopWindow && (
            <label className="icon-picker" title="切换任务栏和托盘图标">
              <img src={iconPreviewSources[iconVariant]} alt="" />
              <span>图标</span>
              <select value={iconVariant} disabled={busy} onChange={(event) => void changeIconVariant(event.target.value as IconVariant)}>
                {iconVariants.map((icon) => <option key={icon.value} value={icon.value}>{icon.label} · {icon.description}</option>)}
              </select>
            </label>
          )}
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

      {message && <div className="toast" role="status"><Icon.Info /><span>{message}</span><button className="toast-close" type="button" aria-label="关闭通知" title="关闭" onClick={() => notify('')}><Icon.X /></button></div>}

      <aside className="sidebar">
        <div className="sb-section">
          <div className="sb-label">订阅分组</div>
          <div className="sub-form">
            <input type="url" value={subscriptionURL} disabled={busy} onChange={(e) => setSubscriptionURL(e.target.value)} placeholder="订阅链接..." />
            <button className="ghost sm" disabled={busy || !subscriptionURL.trim()} onClick={() => void importSubscription()}><Icon.Plus /></button>
          </div>
          <div className="sub-list">
            {subscriptions.map((sub) => (
              <div
                className={`sub-row ${sub.id === displayGroup?.id ? 'active' : ''}`}
                key={sub.id}
                role="button"
                tabIndex={busy || !running || sub.id === displayGroup?.id ? -1 : 0}
                aria-current={sub.id === displayGroup?.id ? 'true' : undefined}
                aria-label={`切换到订阅分组 ${sub.name}`}
                onClick={() => !busy && running && sub.id !== displayGroup?.id && void selectGroup(sub.id)}
                onKeyDown={(event) => {
                  if ((event.target as HTMLElement).closest('button')) return
                  if ((event.key === 'Enter' || event.key === ' ') && !busy && running && sub.id !== displayGroup?.id) {
                    event.preventDefault()
                    void selectGroup(sub.id)
                  }
                }}
              >
                <div className="info">
                  <div className="ic"><Icon.Layers /></div>
                  <div className="meta">
                    <div className="nm">{sub.name}</div>
                    <div className="sub">{sub.nodeCount} 节点 · {sub.updatedAt ? new Date(sub.updatedAt).toLocaleDateString() : '--'}</div>
                  </div>
                </div>
                <div className="btns">
                  <button className="ghost sm" disabled={busy} onClick={(event) => { event.stopPropagation(); void refreshSubscription(sub.id) }}><Icon.Refresh /></button>
                  <button className="danger sm" disabled={busy} onClick={(event) => { event.stopPropagation(); void deleteSubscription(sub.id) }}><Icon.Trash /></button>
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
          <div className="route-sidebar-hint">{routingMode.globalProxy ? '全局代理已开启：所有接入流量都走代理。' : '大陆规则自动直连；未匹配的流量默认走代理。'}</div>
          <div className="sys-proxy" style={{ marginTop: '8px' }}>
            <div>
              <div className="nm">全局代理</div>
              <div className="st">{routingMode.globalProxy ? '已开启 · 全部流量走代理' : '规则分流 · 大陆直连'}</div>
            </div>
            <div
              className={routingMode.globalProxy ? 'toggle on' : 'toggle'}
              role="switch"
              aria-checked={routingMode.globalProxy}
              aria-label="全局代理"
              onClick={() => !busy && hasSubscriptions && void toggleGlobalProxy()}
            />
          </div>
			{tunMode?.supported && (
				<div className="sys-proxy" style={{ marginTop: '8px' }}>
					<div>
						<div className="nm">TUN 模式</div>
						<div className="st">{tunMode.enabled ? '已开启 · 接管应用直连流量' : tunMode.configured ? '已配置 · 需管理员权限才能生效' : tunMode.elevated ? '已关闭 · 可接管 Navicat 等应用' : '需以管理员身份运行'}</div>
					</div>
					<div
						className={tunMode.configured ? 'toggle on' : 'toggle'}
						role="switch"
						aria-checked={tunMode.configured}
						aria-label="TUN 模式"
						title={tunMode.elevated ? '创建虚拟网卡并接管应用直连流量' : '请右键 QingSuo.exe，选择“以管理员身份运行”后再开启'}
						onClick={() => !busy && hasSubscriptions && (tunMode.configured || tunMode.elevated) && void toggleTunMode()}
					/>
				</div>
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
          {autoLaunch?.supported && (
            <div className="sys-proxy" style={{ marginTop: '8px' }}>
              <div>
                <div className="nm">开机自启动</div>
                <div className="st">{autoLaunch.enabled ? '已开启 · 启动后驻留托盘' : '已关闭'}</div>
              </div>
              <div
                className={autoLaunch.enabled ? 'toggle on' : 'toggle'}
                role="switch"
                aria-checked={autoLaunch.enabled}
                aria-label="开机自启动"
                onClick={() => !busy && void toggleAutoLaunch()}
              />
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
                <p>{routingMode.globalProxy ? '全局代理已开启：内置和自定义直连规则暂不生效。' : '自定义白名单匹配域名和子域名并直连；未匹配流量默认走代理。'}</p>
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
                        <span>域名及子域名</span><span className={rule.outbound === '直连' ? 'route-direct' : 'route-disabled'}>{rule.outbound}</span>
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
              {displayGroup && <span className="muted">{displayGroup.auto ? '自动选优' : '已暂停自动'} · {displayGroup.active || '--'}</span>}
            </div>
            <div className="actions">
              <div className="switch-inline auto-select-switch">
                <span>自动选择</span>
                <div
                  className={`toggle ${autoSwitch.autoSelection ? 'on' : ''}`}
                  role="switch"
                  aria-checked={autoSwitch.autoSelection}
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
              <label className="switch-interval" title="自动选优会按此周期重新测速并挑选节点">
                <span>自动切换</span>
                <select value={autoSwitch.switchInterval} disabled={busy} onChange={(event) => void changeSwitchInterval(event.target.value as SwitchInterval)}>
                  {switchIntervals.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
                </select>
              </label>
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
              <button className="sm" disabled={busy || !running || !displayGroup} onClick={() => void testNodes()} title="按当前测速项目测试，只测试当前订阅分组"><Icon.Zap /> 测全部</button>
            </div>
          </div>
          {hasSubscriptions && nodesState && (
            <>
              <div className="filter-bar">
                <label>测速项目</label>
                <select value={testService} onChange={(e) => setTestService(e.target.value as TestService)}>
                  <option value="all">全部服务</option>
                  <option value="google">Google</option>
                  <option value="gemini">Gemini</option>
                  <option value="chatgpt">ChatGPT</option>
                </select>
                <label>排序</label>
                <select value={nodeSort} onChange={(e) => setNodeSort(e.target.value as NodeSort)}>
                  <option value="combined-asc">综合延迟↑</option>
                  <option value="combined-desc">综合延迟↓</option>
                  <option value="google-asc">Google 延迟↑</option>
                  <option value="gemini-asc">Gemini 延迟↑</option>
                  <option value="chatgpt-asc">ChatGPT 延迟↑</option>
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
              return (
                <div className={node.tag === displayGroup?.active ? 'nrow on' : 'nrow'} key={node.tag}>
                  <span className="nm">{node.name}{node.tag === displayGroup?.active && <span className="badge">活跃</span>}</span>
                  <span className="ctry">{node.country}</span>
                  <span className="service-latencies" title={node.error || '实际服务延迟：Google、Gemini、ChatGPT'}>
                    <span className={`lat ${node.googleDelayMs > 0 ? 'ok' : node.googleDelayMs === -1 ? 'bad' : 'idle'}`}>G {formatDelay(node.googleDelayMs)}</span>
                    <span className={`lat ${node.geminiDelayMs > 0 ? 'ok' : node.geminiDelayMs === -1 ? 'bad' : 'idle'}`}>Gemini {formatDelay(node.geminiDelayMs)}</span>
                    <span className={`lat ${node.chatgptDelayMs > 0 ? 'ok' : node.chatgptDelayMs === -1 ? 'bad' : 'idle'}`}>GPT {formatDelay(node.chatgptDelayMs)}</span>
                  </span>
                  <span className="btns">
                    <button className="ghost sm" disabled={busy || !running} onClick={() => void testNodes(node.tag)} title="按当前测速项目测试">测速</button>
                    <button className="sm" disabled={busy || !running} onClick={() => displayGroup && void chooseNode(displayGroup.id, 'node', node.tag)} title="立即使用此节点，仍保持自动选择">使用</button>
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
              ? <div className="hint"><Icon.FileCode /> 配置由订阅分组自动管理 · <code>proxy</code> 选择器 + <code>urltest</code> · {tunMode?.enabled ? 'TUN 已开启：应用直连流量也会接入' : ''}{tunMode?.enabled && ' · '}{routingMode.globalProxy ? '全局代理：全部流量走当前节点' : '国内走白名单直连'}</div>
              : <textarea value={config} disabled={running} onChange={(e) => setConfig(e.target.value)} spellCheck="false" />
          ) : (
            <pre ref={logRef}>{logs || '尚无日志。'}</pre>
          )}
        </div>
      </section>
    </main>
  )
}
