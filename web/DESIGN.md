---
name: Coordica
description: 单项目多 Agent 协作工作台
colors:
  canvas: "#f4f6fa"
  surface: "#ffffff"
  subtle: "#edf1f7"
  ink: "#182236"
  muted: "#53627a"
  border: "#d7dfea"
  accent: "#2456d9"
  accent-hover: "#1c43b0"
  selection: "#e9efff"
  success: "#23633e"
  success-bg: "#eaf5ee"
  warning: "#80520b"
  warning-bg: "#fff4da"
  failure: "#a52c37"
  failure-bg: "#ffedf0"
  dock: "#182236"
  dock-hover: "#26344c"
  dock-text: "#dfe8f8"
typography:
  title:
    fontFamily: "'SF Pro Text', 'PingFang SC', 'Segoe UI', 'Microsoft YaHei', sans-serif"
    fontSize: "28px"
    fontWeight: 600
    lineHeight: 1.25
  section:
    fontFamily: "'SF Pro Text', 'PingFang SC', 'Segoe UI', 'Microsoft YaHei', sans-serif"
    fontSize: "16px"
    fontWeight: 600
    lineHeight: 1.45
  body:
    fontFamily: "'SF Pro Text', 'PingFang SC', 'Segoe UI', 'Microsoft YaHei', sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.5
  secondary:
    fontFamily: "'SF Pro Text', 'PingFang SC', 'Segoe UI', 'Microsoft YaHei', sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
  small:
    fontFamily: "'SF Pro Text', 'PingFang SC', 'Segoe UI', 'Microsoft YaHei', sans-serif"
    fontSize: "12px"
    fontWeight: 400
    lineHeight: 1.6
rounded:
  control: "5px"
spacing:
  step-1: "4px"
  step-2: "8px"
  step-3: "12px"
  step-4: "16px"
  step-6: "24px"
components:
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.surface}"
    rounded: "{rounded.control}"
    padding: "9px 16px"
  button-primary-hover:
    backgroundColor: "{colors.accent-hover}"
  input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: "9px 12px"
---
# Design System: Coordica

## Overview

**Creative North Star: "项目档案工作台"**

工作台服务于日常推进与处理问题。项目、具体列表与真实执行实例优先，视觉来自清楚的排版、状态和对齐。用户明确反对泛化灰绿；本轮受托设计使用纸白工作面、浅冷背景、深墨 Dock 和蓝色操作。功能与权限是产品事实，颜色不能代替执行证据。

## Colors

颜色的规范值在frontmatter与src/styles.css。accent用于主操作、当前选择、focus和caret；canvas与surface区分背景和工作区，muted仍保证可读性。success只传达实际成功或可用性，warning用于待处理/暂停，failure用于失败。普通通知用信息态，已取消使用中性态。状态始终带文字，能力探测不代表认证或真实运行。

## Typography

系统无衬线统一标题、正文、标签与控件；标题28px、区域标题16px、正文及表格14px，次要说明13px，技术文字12px。标题600、tracking -.025em，行高1.25；正文继承1.5，段落1.8。代码/OID使用等宽，时间和计数用tabular数字。没有装饰性eyebrow或巨大指标数字。

## Layout

桌面正文限宽1480px、左右32px；640px以下16px边距。首页四池2×2，主机动态在右侧；1000px以下主机移至正文下，640px以下池单列。目标默认简洁、可展开原文；高级信息渐进展开，普通行优先标题、状态和责任。宽表仅内部滚动，长ID可换行。底部预留112px与safe-area，Dock不覆盖末尾内容。

## Elevation & Depth

内容工作面平坦，以底色、细分隔和一致标题分组；不堆叠卡片或用阴影代替内容。仅浮起Dock使用真实偏移的柔和阴影，值记录在sidecar。

## Shapes

标准控件5px圆角，底部Dock12px圆角；列表分隔与原生输入/下拉保持熟悉操作。小状态标签保留语义文字，不把所有元素胶囊化。图标使用既有1.6px内联SVG。

## Components

主按钮蓝底白字，hover加深，危险动作保留红色；次按钮白底边框，text-button使用蓝色。输入统一padding与边框，caret/focus与主色一致。焦点2px、offset3px；文字选中使用selection/ink。disabled不冒充可操作状态，错误与成功反馈保留真实请求结果。

Dock桌面八项，小屏五项并提供工作池内切换；深墨底、浅色文字，当前项蓝底白字，保留aria-current与键盘操作。无常驻顶部/侧边全局导航，无毛玻璃、霓虹或入场编排。reduced-motion取消无必要过渡。

## Do's and Don'ts

- Do 保持用户原文、助手摘要和来源可查，技术证据按需展开，未知值保留。
- Do 区分工具能力与真实实例、历史报告与当前固定版本、缺失与通过。
- Do 复用现有特性组件、语义token和真实API，不用视觉状态制造授权或成功。
- Don't 用灰绿表示导航、普通进度、焦点或一般提示，不伪造可保存配置。
- Don't 把桌面/手机浏览器确认说成所有平台原生部署验收；jsdom不是浏览器E2E。
