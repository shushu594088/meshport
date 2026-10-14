---
version: alpha
name: Meshport
description: A quiet network control desk for personal Linux servers.
colors:
  primary: "#176B87"
  background: "#EFF3F6"
  surface: "#FFFFFF"
  ink: "#182C3C"
  muted: "#526775"
  border: "#D3DFE6"
  success: "#23755B"
  warning: "#90601A"
  danger: "#B33E43"
typography:
  sans:
    fontFamily: '"Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif'
  display:
    fontFamily: '"Trebuchet MS", "Segoe UI", "PingFang SC", sans-serif'
  mono:
    fontFamily: '"Cascadia Code", Consolas, monospace'
rounded:
  DEFAULT: "0.625rem"
  panel: "1rem"
spacing:
  page-max: "76rem"
  section-gap: "1.5rem"
components:
  button:
    height: "2.75rem"
  card:
    rounded: "1rem"
  dialog:
    width: "34rem"
---

# Meshport Design System

## Overview

Meshport is a product/admin interface for a single server administrator, grounded in README.md. Chinese is the initial interface language (zh-CN); no Japan-specific market assumptions apply. Its reference is a network patch panel: distinct management, proxy and system traffic paths. The signature is a small, labeled connection diagram on the overview, explaining that the management service stays reachable when the proxy stops. Avoid decorative analytics, invented traffic numbers and marketing heroes.

The tokens above map to web/src/style.css :root variables, one-to-one. CSS is the runtime adapter; shared components consume variables. Changes to durable values must update both files. No remote fonts or third-party runtime assets are needed.

## Colors

Cool gray surfaces, white work panels and deep blue text keep operational controls calm. Teal identifies the selected page and primary actions. Green means running, amber means awaiting confirmation, red means failed or destructive. Always include a text label. Only a light theme is supported; forced-colors uses system colors.

## Typography

Body text uses the local sans stack at 15–16px with 1.65 line height. Display text uses the display stack at 28–38px, kept to page titles and the brand. Addresses and ports use the mono stack with tabular numerals. Chinese text wraps naturally; identifiers wrap instead of truncating secret-bearing URLs.

## Layout

Desktop: a 220px sidebar and a centered workspace up to 76rem wide. At 800px navigation becomes a horizontal strip; at 560px paired panels and fields stack. Document scrolling owns long forms. Node and subscription lists use bounded local pages of 20 records from complete API datasets; filters and page state are reflected in URL fragments. Spacing is built from 6, 12, 18 and 24px steps.

## Elevation & Depth

Panels use a border; only dialogs use a soft shadow. Flat operational screens keep attention on status and control. Dialog backdrop isolates the active task.

## Shapes

Controls use 0.625rem corners and panels 1rem. The connection diagram is the sole expressive shape. Icons are simple stroke SVGs alongside text, never the only explanation of an action.

## Components

Buttons have solid primary, neutral outline and separated danger variants. Minimum height 44px, explicit hover, focus and disabled states; a global busy state prevents duplicate mutations without changing label widths. Fields reserve help/error space. SecretField owns masked values and reveal controls; subscription URLs use it too because URLs may carry credentials.

Dialog.vue uses native dialog.showModal for inert background and focus containment, with app-owned content, explicit accessible titles, Escape, safe initial focus and restoration. Feedback is persistent inline for errors and global live status for success. No native browser confirmation dialogs.

Diagnostics reuse the existing panel, button and semantic color tokens. Website cards use three columns on desktop, two on smaller workspaces and one on phones; each keeps a stable status, explanation and timestamp region. “支持”, “不支持”, “可访问” and “测试失败” always include labels and icons. Node selection and testing are separate buttons. VersionPanel reuses Dialog.vue for update confirmation; dialogs use unique label IDs so multiple mounted dialogs remain accessible.

Motion is limited to a 150ms state transition and a spinner; prefers-reduced-motion disables it. All dates use zh-CN formatting in the browser's timezone, displayed alongside an explicit timezone label. No fabricated charts or statistics.

## Do's and Don'ts

- Do keep management reachability and kernel health separate.
- Do explain disabled actions, preserve failed form input and acknowledge only server-confirmed changes.
- Don't display secret keys, complete subscription URLs or proxy passwords in lists or messages.
- Don't use color alone, unbounded node grids, or decorative motion for network changes.
