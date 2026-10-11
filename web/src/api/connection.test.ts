import { describe, expect, it } from 'vitest';
import { connectionInstruction, localConsoleURL, remoteRunnerURL, validateConnectionInfo } from './connection';
import { connectionInfo } from '../test/connectionInfo';

describe('connection configuration boundaries', () => {
  it('uses only literal loopback HTTP roots for local console navigation', () => {
    expect(localConsoleURL('http://127.0.0.1:8080')).toBe('http://127.0.0.1:8080');
    expect(localConsoleURL('http://[::1]:8080/')).toBe('http://[::1]:8080');
    for (const value of ['https://127.0.0.1:8080', 'http://localhost:8080', 'http://example.com:8080', 'javascript:alert(1)', 'http://user:secret@127.0.0.1:8080', 'http://127.0.0.1:8080/api', 'http://127.0.0.1:8080?token=secret']) {
      expect(localConsoleURL(value)).toBeUndefined();
    }
  });

  it('requires a usable remote HTTPS address and rejects wildcard or loopback addresses', () => {
    expect(remoteRunnerURL(connectionInfo.runner_url)).toBe(connectionInfo.runner_url);
    expect(remoteRunnerURL('https://center.example.com/')).toBe('https://center.example.com');
    for (const value of ['', 'http://192.0.2.1:8443', 'http://127.0.0.1:8080', 'https://127.0.0.1:8443', 'https://localhost:8443', 'https://localhost.:8443', 'https://*:8443', 'https://*.example.com', 'https://0.0.0.0:8443', 'https://[::]:8443', 'https://[::1]:8443', 'https://[::ffff:127.0.0.1]:8443', 'https://secret@center.example.com', 'https://center.example.com/path']) {
      expect(remoteRunnerURL(value)).toBeUndefined();
    }
  });

  it('serializes only public connection fields and the one-time pairing code', () => {
    const source = { ...connectionInfo, owner_token: 'private-owner', token: 'private-node' };
    const receipt = { code: 'pair_one', expires_at: '2030-01-01T00:00:00Z', owner_token: 'private-pairing-token' };
    const result = connectionInstruction(source, receipt, 'Windows-dsh');
    const config = JSON.parse(result.text.split('```json\n')[1].split('\n```')[0]);
    expect(config).toEqual({ version: connectionInfo.version, center_url: connectionInfo.runner_url,
      ca_certificate_pem: connectionInfo.certificate_pem, ca_sha256: connectionInfo.certificate_sha256,
      pairing_code: receipt.code, expires_at: receipt.expires_at, name: 'Windows-dsh' });
    expect(result.text).not.toContain('private-');
    expect(result.text).not.toContain('data_dir');
    expect(result.text).toContain('证书 DER');
    expect(result.text).toContain('不是 PEM 文件本身的哈希');
  });

  it('rejects malformed deployment metadata and private key material', () => {
    expect(validateConnectionInfo(connectionInfo)).toBe(connectionInfo);
    expect(() => validateConnectionInfo({ ...connectionInfo, api_version: 'v2' })).toThrow('契约不兼容');
    expect(() => validateConnectionInfo({ ...connectionInfo, certificate_pem: '-----BEGIN PRIVATE KEY-----' })).toThrow('公共证书格式无效');
  });
});
