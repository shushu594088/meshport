# Meshport UX Contract

## Product context

Single administrator, zh-CN, operational language. Job: manage Linux server subscriptions and proxy behavior while keeping the management service available. Dates use browser timezone, shown explicitly. Native semantics, keyboard focus and WCAG 2.2 AA are the target.

## Business-context sources

| Domain | Source | Type | Reviewed |
|---|---|---|---|
| Authentication, initialization, secret handling | README.md 架构与配置管理 | Product specification | 2026-10-13 |
| Subscription lifecycle and deletion | README.md 订阅与节点 | Product specification | 2026-10-13 |
| Service independence, persisted settings | README.md 架构与配置管理 | Product specification | 2026-10-13 |
| Diagnostics and program updates | README.md 节点测速、网站测试与版本更新; docs/DEVELOPMENT.md | Product/API specification | 2026-10-14 |
| TUN confirmation and recovery | README.md TUN 与故障恢复 | Product specification | 2026-10-13 |

## Visual contract

DESIGN.md owns visual intent and tokens; web/src/style.css implements them. Shared Vue primitives own repeated behavior. Light theme with reduced-motion and forced-colors paths.

## Canonical UI Map

| Capability | Canonical owner | Source of truth | Allowed variants | Verification |
|---|---|---|---|---|
| Select/Listbox | web/src/components/SelectField.vue + ModeControl.vue | README.md and this contract | native popup (platform geometry accepted); native radios | browser keyboard and popup |
| Form | web/src/composables/useForm.ts + Field.vue + SecretField.vue | this contract | create / settings / auth | browser success and validation |
| Scrollbar | web/src/style.css | DESIGN.md | global native baseline | computed style |
| Toast | web/src/App.vue shared status region | this contract | success / error / warning | browser live region |
| CRUD | web/src/api.ts + App.vue mutation wrapper | README.md and this contract | refresh owning view | CRUD browser tests |

## Dataset navigation

Subscriptions max 64 server-side. Nodes and subscriptions show 20 records per page, with local IME-safe filtering against full returned datasets. Hash query stores page/search; secrets never enter URLs or localStorage. Clearing search is immediate and returns focus to input. Empty, no-results and service-offline states provide actionable copy.

## Flow ledger

| Operation | Pending | Success | Failure recovery | Source |
|---|---|---|---|---|
| Add subscription | disable duplicate submit | close dialog, refresh list, live status | dialog stays open, fields preserved | README.md 订阅与节点 |
| Update / activate subscription | global busy | refresh list and overview | inline persistent error, previous config remains | README.md 架构与配置管理 |
| Delete subscription | named app-owned confirmation | refresh list, clamp page | confirmation stays open | README.md 订阅与节点 |
| Save settings | global busy | refresh state, show new port link | preserve draft and focus error; revision conflicts require reload | README.md 架构与配置管理 |
| Enable TUN | confirmation describes 60s window | persistent countdown and confirmation action | automatic server-side recovery | README.md TUN 与故障恢复 |
| Choose node | global busy | server result then selected label | keep current selection | README.md 订阅与节点 |
| Change password | disable duplicate submit | re-login with new password | retain fields and show error | README.md 管理员登录 |
| Test node delays / websites | four concurrent requests, per-item pending and progress, cancel | preserve per-item result in memory | retain partial results, individual retry; reject changed configuration | README.md 节点测速、网站测试与版本更新 |
| Check / install update | explicit check; app-owned restart confirmation | reconnect, server-confirmed completion and version | checksum/package failure leaves installation intact; install/health failure restores program files | README.md 节点测速、网站测试与版本更新 |

## Navigation and responsive behavior

Five hash routes: overview, subscriptions, nodes, tests, settings. Titles follow the active route. Settings drafts warn before in-app route changes via shared Dialog; beforeunload handles browser unload only. Nodes use cards so long identifiers wrap. Below 800px horizontal navigation replaces sidebar; below 560px forms stack.

## Overlays and feedback

Dialog.vue is canonical, with safe initial focus, native modal focus containment, Escape and restored trigger focus. Deletion and TUN/external proxy exposure require app-owned confirmation naming consequences. Routine saves do not. Errors stay visible until retry/dismissal; live status announces completed mutations, never secrets. Dialog layer precedes global status.

## Async and resilience

Mutations are pessimistic with one shared busy owner. API fetches have a 60s timeout. Refresh uses request sequencing to ignore stale responses and never overwrites an edited settings draft. On offline errors cached data remains visible with an error. On 401 the auth view opens; settings draft stays in memory, secrets are not persisted. On 409 settings remain for review, refresh resolves the revision. TUN pending state is queried every 2s without relying on browser connectivity for recovery.

Diagnostics use a shared useDiagnostics queue with at most four requests, AbortSignal cancellation, per-item failures and no automatic retry. Test results are transient and cleared when configuration revision changes. Node batches cover all actual nodes, regardless of the current group, filter or page. Website probes use the server HTTP proxy and current routing mode; accessible pages alone do not imply full media/account unlock. Update status lives on the server; VersionPanel remains mounted across route changes, polls progress every 2s and tolerates the backend restart. Update UI requires saved settings and no pending TUN; root CLI also refuses pending TUN. No update is installed merely by checking a version.

## Validation

useForm is the shared validation adapter. Forms use novalidate, first invalid field focus, visible field errors with aria-invalid and aria-describedby. Server errors preserve input. SecretField masks values by default with an accessible show/hide button; autocomplete follows credential roles. Proxy password blank preserves the stored value. Subscription URL is masked as a potentially sensitive value.

## Verification

Go unit/integration tests cover authentication, state persistence, config ownership and failure rollback. tests/e2e covers browser CRUD, settings, errors, keyboard and mobile layout; server2 acceptance covers real Mihomo, systemd independence, ports and TUN rollback. npm --prefix web run typecheck and build are required. The frontend premium strict auditor and DESIGN.md lint verify static ownership and tokens; runtime evidence remains separate.
