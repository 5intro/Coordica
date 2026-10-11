import type { ConnectionInfo } from '../api.generated';
import { ApiError } from './client';

function rootURL(value: string | undefined): URL | undefined {
  if (!value) return;
  try {
    const url = new URL(value);
    if (url.username || url.password || url.search || url.hash || url.pathname !== '/') return;
    return url;
  } catch { return; }
}

function loopback(host: string) {
  host = host.replace(/\.$/, '');
  return /^127\./.test(host) || host === '[::1]' || host === 'localhost' || host.endsWith('.localhost')
    || /^\[::ffff:7f[0-9a-f]{2}:/.test(host);
}

// Navigation stays on literal loopback HTTP. A server-provided link is not
// permission to fetch an owner's API or carry credentials across origins.
export function localConsoleURL(value: string | undefined): string | undefined {
  const url = rootURL(value);
  if (url?.protocol !== 'http:' || !(/^127\.\d+\.\d+\.\d+$/.test(url.hostname) || url.hostname === '[::1]')) return;
  return url.origin;
}

export function remoteRunnerURL(value: string): string | undefined {
  const url = rootURL(value);
  if (url?.protocol !== 'https:' || url.hostname.includes('*') || loopback(url.hostname) || url.hostname === '0.0.0.0'
    || url.hostname === '[::]' || url.hostname === '[::ffff:0:0]') return;
  return url.origin;
}

export function validateConnectionInfo(value: ConnectionInfo): ConnectionInfo {
  if (!value || value.api_version !== 'v1' || !(['version', 'console_url', 'runner_url', 'runner_console_url',
    'console_listen', 'runner_listen', 'data_dir', 'certificate_pem', 'certificate_sha256'] as const)
    .every((key) => typeof value[key] === 'string')) {
    throw new ApiError('incompatible_response', '中心连接配置契约不兼容，请更新工作台。');
  }
  if (value.certificate_pem && (!value.certificate_pem.includes('-----BEGIN CERTIFICATE-----')
    || value.certificate_pem.includes('PRIVATE KEY'))) {
    throw new ApiError('incompatible_response', '中心公共证书格式无效，已停止展示。');
  }
  return value;
}

export interface PairingReceipt { code: string; expires_at: string }
export interface GeneratedConnection { text: string; expiresAt: string; name: string }

export function connectionInstruction(info: ConnectionInfo, pairing: PairingReceipt, name: string): GeneratedConnection {
  const center = remoteRunnerURL(info.runner_url);
  if (!center) throw new ApiError('invalid_connection', '请先为中心配置可供其他电脑访问的 HTTPS Runner 入口。');
  if (!pairing || typeof pairing.code !== 'string' || !pairing.code || typeof pairing.expires_at !== 'string'
    || !Number.isFinite(new Date(pairing.expires_at).getTime())) {
    throw new ApiError('invalid_response', '中心没有返回有效的一次性配对回执，请刷新后重试。');
  }
  // Select fields explicitly. Owner credentials, node credentials, and center
  // filesystem paths must never be part of a configuration sent elsewhere.
  const config = {
    version: info.version,
    center_url: center,
    ca_certificate_pem: info.certificate_pem,
    ca_sha256: info.certificate_sha256,
    pairing_code: pairing.code,
    expires_at: pairing.expires_at,
    name,
  };
  const text = `请在当前电脑安装 Coordica 的 MCP，并初始化后台 Runner，加入这个中心的多 Agent 协作。
项目地址：https://github.com/5intro/Coordica；使用与下面 version 一致、适合当前系统的发行包。已有安装时优先复用。

以下接入配置只供一个节点使用一次，请在 expires_at 前完成初始化：
\`\`\`json
${JSON.stringify(config, null, 2)}
\`\`\`

请选择固定的本机程序安装位置和 Runner 数据目录，注册 MCP 后调用 inspect_environment。
若 ca_certificate_pem 非空，将公共 CA 写入本机固定绝对路径的 .crt 文件，并核对证书 DER 的 SHA-256 指纹与 ca_sha256（不是 PEM 文件本身的哈希）。
使用 initialize_runner，传入 center_url、pairing_code、name、capacity: 1；有公共 CA 时将该文件的绝对路径传入 tls_ca。
若本机已有配对，请保持原 Runner 数据目录和节点身份，确认属于同一中心；不要覆盖或删除现有配置。
完成后报告节点编号、后台服务状态及实际 Agent 工具能力，不要输出任何 token、密钥或其他登录凭据。
`;
  return { text, expiresAt: pairing.expires_at, name };
}
