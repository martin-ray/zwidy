# Zwidy 手動動作確認メモ

このドキュメントは、`zwidyd` の TLS・証明書・server/client 起動確認を行うための最小手順です。

## 前提

- 日付基準: 2026年8月18日時点
- Linux
- `openssl`
- `ip`
- `/dev/net/tun`
- `CAP_NET_ADMIN` 相当

## 1. TLS / 証明書だけを確認する

Go 側の TLS 設定確認は自動テストで実施できます。

```bash
/usr/local/go/bin/go test ./internal/transport -run TestServerAndClientTLSConfigHandshakeWithClientCert -v
```

このテストでは以下を確認します。

- 自前 CA でサーバ証明書とクライアント証明書を生成
- `internal/transport` の `ServerTLSConfig` / `ClientTLSConfig` を利用
- サーバ証明書検証が通る
- クライアント証明書がサーバへ提示される

## 2. PKI を作る

```bash
./scripts/gen-pki.sh init-ca --pki-dir ./tmp-pki --ca-cn "Zwidy Test CA"
./scripts/gen-pki.sh issue-server --pki-dir ./tmp-pki --name tunnel.example.com --dns tunnel.example.com --ip 127.0.0.1
./scripts/gen-pki.sh issue-client --pki-dir ./tmp-pki --name jellyfin-home
```

## 3. 設定ファイルを作る

同一ホストで server と client を起動する場合、TUN 名は衝突しないように分けます。

`/tmp/zwidy-server.yaml`:

```yaml
mode: server

listen:
  address: 127.0.0.1
  port: 51820
  transport: tcp

network:
  interface: zwidys0
  cidr: 10.77.0.0/24
  server_ip: 10.77.0.1
  mtu: 1380
  client_to_client: false
  max_packet_size: 65535

ipam:
  database: /tmp/zwidy-test.db

tls:
  certificate: /absolute/path/to/tmp-pki/server/tunnel.example.com.crt
  private_key: /absolute/path/to/tmp-pki/server/tunnel.example.com.key
  ca: /absolute/path/to/tmp-pki/ca/ca.crt

logging:
  level: debug
  format: text
  output: stdout

metrics:
  enabled: true
  address: 127.0.0.1:19090

keepalive:
  interval: 15s
  timeout: 45s

clients:
  - node_id: jellyfin-home
    ip: 10.77.0.10
    token: change-me
```

`/tmp/zwidy-client.yaml`:

```yaml
mode: client

node:
  id: jellyfin-home

server:
  address: 127.0.0.1
  port: 51820
  transport: tcp

network:
  interface: zwidyc0
  cidr: 10.77.0.0/24
  mtu: 1380
  max_packet_size: 65535

auth:
  token: change-me
  credential_file: /absolute/path/to/tmp-pki/clients/jellyfin-home.crt
  private_key_file: /absolute/path/to/tmp-pki/clients/jellyfin-home.key

tls:
  ca: /absolute/path/to/tmp-pki/ca/ca.crt
  server_name: tunnel.example.com

logging:
  level: debug
  format: text
  output: stdout

metrics:
  enabled: true
  address: 127.0.0.1:19091

reconnect:
  enabled: true
  min_delay: 1s
  max_delay: 5s

keepalive:
  interval: 15s
  timeout: 45s
```

## 4. 起動確認

```bash
./zwidyd server --config /tmp/zwidy-server.yaml
./zwidyd client --config /tmp/zwidy-client.yaml
```

期待するログ:

- server 側で `client connected`
- client 側で `client connected`
- server 側で `virtual_ip=10.77.0.10`

## 5. 注意点

- 同一ホストでの full tunnel 確認は route 競合で不安定です
- 本当に確認したいのは、server と client を別ホスト、または別 network namespace に分けた構成です
- その場合は `ping 10.77.0.10` や `ip route` を合わせて確認してください

## 6. 現実的な確認順序

1. TLS 自動テスト
2. ローカル起動で `client connected` まで確認
3. 別ホスト / namespace で packet forwarding を確認
