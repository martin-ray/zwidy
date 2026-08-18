# Zwidy PKI / 証明書運用ガイド

このドキュメントでは、`zwidyd` 用のサーバ証明書・クライアント証明書・CA 証明書を安全寄りに生成する手順をまとめます。

方針としては、OpenVPN 系の運用でよくある **自前 CA を作り、サーバ証明書とクライアント証明書を個別発行する** 形です。
ただし、古い手順で見かけることがある以下は避けます。

- サーバ証明書とクライアント証明書の使い回し
- `Common Name` だけに依存したサーバ名検証
- `subjectAltName` なしのサーバ証明書
- 用途制限 (`extendedKeyUsage`) なしの証明書
- 長期間使い回す 1024-bit RSA 鍵

`zwidyd` の現在実装に合わせると、証明書まわりの考え方は次の通りです。

- サーバは TLS 証明書を提示する
- クライアントは CA 証明書でサーバ証明書を検証する
- クライアントは任意でクライアント証明書を提示できる
- **現状の `zwidyd` はクライアント証明書の提示自体は可能ですが、証明書の識別子でクライアント認可まではしていません**
- そのため、**現時点では node ごとの enrollment token とクライアント証明書を併用**するのが実用的です

将来的に `zwidyd` 側で mTLS 必須化と証明書ベース認可を入れれば、そのままより強い構成へ移行できます。

## 推奨構成

最小でも以下を用意します。

- オフライン気味に保管する CA 鍵: `ca.key`
- クライアント配布用 CA 証明書: `ca.crt`
- サーバ秘密鍵: `server.key`
- サーバ証明書: `server.crt`
- クライアント秘密鍵: `clients/<node>.key`
- クライアント証明書: `clients/<node>.crt`
- 証明書失効リスト: `crl.pem`（任意だが推奨）

推奨ディレクトリ例:

```text
pki/
├── ca/
│   ├── ca.key
│   ├── ca.crt
│   ├── index.txt
│   ├── serial
│   ├── crlnumber
│   └── openssl.cnf
├── server/
│   ├── server.key
│   ├── server.csr
│   └── server.crt
├── clients/
│   ├── jellyfin-home.key
│   ├── jellyfin-home.csr
│   ├── jellyfin-home.crt
│   └── ...
└── crl.pem
```

## 前提

以下のツールを使います。

- `openssl`

OpenSSL 3 系を前提にしていますが、1.1.1 系でも大半はそのまま使えます。

## 1. CA を作る

まず、CA 作業用ディレクトリを作ります。

```bash
mkdir -p pki/ca pki/server pki/clients
cd pki/ca
: > index.txt
printf '1000\n' > serial
printf '1000\n' > crlnumber
```

次に OpenSSL 設定ファイルを作ります。

`pki/ca/openssl.cnf`:

```ini
[ ca ]
default_ca = CA_default

[ CA_default ]
dir               = .
certs             = $dir
new_certs_dir     = $dir
crl_dir           = $dir
certificate       = $dir/ca.crt
private_key       = $dir/ca.key
database          = $dir/index.txt
serial            = $dir/serial
crlnumber         = $dir/crlnumber
crl               = $dir/../crl.pem
default_md        = sha256
default_days      = 825
default_crl_days  = 30
policy            = policy_loose
x509_extensions   = v3_ca
copy_extensions   = copy
unique_subject    = no

[ policy_loose ]
commonName              = supplied
stateOrProvinceName     = optional
countryName             = optional
organizationName        = optional
organizationalUnitName  = optional
emailAddress            = optional

[ req ]
default_bits       = 4096
default_md         = sha256
prompt             = no
distinguished_name = dn
x509_extensions    = v3_ca

[ dn ]
CN = Zwidy Internal CA

[ v3_ca ]
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid:always,issuer
basicConstraints = critical, CA:true
keyUsage = critical, keyCertSign, cRLSign

[ server_cert ]
basicConstraints = CA:false
nsCertType = server
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid,issuer
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth

[ client_cert ]
basicConstraints = CA:false
nsCertType = client
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid,issuer
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
```

CA 秘密鍵と CA 証明書を作成します。

```bash
openssl genrsa -out ca.key 4096
chmod 600 ca.key

openssl req -x509 -new -nodes \
  -key ca.key \
  -sha256 \
  -days 3650 \
  -out ca.crt \
  -config openssl.cnf
```

補足:

- `ca.key` は最重要ファイルです
- 可能ならサーバに置かず、管理端末で保管してください
- バックアップも暗号化したうえで保管してください

## 2. サーバ証明書を作る

サーバ証明書には **必ず `subjectAltName` を入れます**。

例として、Zwidy サーバの公開名が `tunnel.example.com`、必要なら固定 IP が `203.0.113.10` だとします。

```bash
cd ../server
openssl genrsa -out server.key 4096
chmod 600 server.key
```

CSR 用の設定ファイルを作ります。

`pki/server/server.cnf`:

```ini
[ req ]
default_bits       = 4096
prompt             = no
default_md         = sha256
distinguished_name = dn
req_extensions     = req_ext

[ dn ]
CN = tunnel.example.com

[ req_ext ]
subjectAltName = @alt_names

[ alt_names ]
DNS.1 = tunnel.example.com
IP.1 = 203.0.113.10
```

CSR を生成します。

```bash
openssl req -new \
  -key server.key \
  -out server.csr \
  -config server.cnf
```

CA で署名します。

```bash
cd ../ca
openssl ca \
  -batch \
  -config openssl.cnf \
  -extensions server_cert \
  -in ../server/server.csr \
  -out ../server/server.crt
```

検証します。

```bash
openssl verify -CAfile ca.crt ../server/server.crt
openssl x509 -in ../server/server.crt -noout -text | grep -A2 'Subject Alternative Name'
```

## 3. クライアント証明書を作る

ノードごとに個別証明書を発行します。

ここでは `jellyfin-home` という node を例にします。

```bash
cd ../clients
openssl genrsa -out jellyfin-home.key 4096
chmod 600 jellyfin-home.key
```

`pki/clients/jellyfin-home.cnf`:

```ini
[ req ]
default_bits       = 4096
prompt             = no
default_md         = sha256
distinguished_name = dn

[ dn ]
CN = jellyfin-home
```

CSR を作成します。

```bash
openssl req -new \
  -key jellyfin-home.key \
  -out jellyfin-home.csr \
  -config jellyfin-home.cnf
```

CA で署名します。

```bash
cd ../ca
openssl ca \
  -batch \
  -config openssl.cnf \
  -extensions client_cert \
  -in ../clients/jellyfin-home.csr \
  -out ../clients/jellyfin-home.crt
```

検証します。

```bash
openssl verify -CAfile ca.crt ../clients/jellyfin-home.crt
openssl x509 -in ../clients/jellyfin-home.crt -noout -text | grep -A2 'Extended Key Usage'
```

## 4. Zwidy に配置するファイル

### サーバ側

サーバに必要なファイル:

- `server.crt`
- `server.key`
- 必要なら `ca.crt`

例:

```text
/etc/zwidy/server.crt
/etc/zwidy/server.key
/etc/zwidy/ca.crt
```

サーバ設定例:

```yaml
mode: server

listen:
  address: 0.0.0.0
  port: 51820
  transport: tcp

network:
  interface: zwidy0
  cidr: 10.77.0.0/24
  server_ip: 10.77.0.1
  mtu: 1380
  client_to_client: false

ipam:
  database: /var/lib/zwidy/zwidy.db

tls:
  certificate: /etc/zwidy/server.crt
  private_key: /etc/zwidy/server.key
  ca: /etc/zwidy/ca.crt

clients:
  - node_id: jellyfin-home
    ip: 10.77.0.10
    token: change-me
```

### クライアント側

クライアントに必要なファイル:

- `ca.crt`
- `jellyfin-home.crt`
- `jellyfin-home.key`

例:

```text
/etc/zwidy/ca.crt
/etc/zwidy/client.crt
/etc/zwidy/client.key
```

クライアント設定例:

```yaml
mode: client

node:
  id: jellyfin-home

server:
  address: tunnel.example.com
  port: 51820
  transport: tcp

network:
  interface: zwidy0
  cidr: 10.77.0.0/24
  mtu: 1380

auth:
  token: change-me
  credential_file: /etc/zwidy/client.crt
  private_key_file: /etc/zwidy/client.key

tls:
  ca: /etc/zwidy/ca.crt
  server_name: tunnel.example.com
```

## 5. サーバ証明書検証で重要な点

クライアント側でサーバ証明書を正しく検証するには、以下が必要です。

- `tls.ca` に正しい CA 証明書を指定する
- `server.address` と `tls.server_name` をサーバ証明書の `subjectAltName` と一致させる
- IP 接続するなら `subjectAltName` に IP も入れる

特に重要なのは、**証明書の CN だけではなく SAN で一致させる**ことです。

悪い例:

- 証明書は `CN=tunnel.example.com` だけ
- クライアントは `203.0.113.10` へ接続
- SAN に `IP:203.0.113.10` がない

この場合、正しく検証すると失敗します。

## 6. クライアント証明書運用の考え方

現時点の `zwidyd` 実装では、クライアント証明書は「提示可能」ですが、サーバ側で `node_id` と証明書主体の強い突合まではしていません。

そのため現実的には次を推奨します。

- **必須:** node ごとの token を設定する
- **推奨:** node ごとのクライアント証明書も配る
- **将来拡張:** サーバで client cert 必須 + `node_id` と証明書の CN/SAN を照合

当面の運用ルールとしては、少なくとも以下を守ると安全です。

- クライアントごとに鍵を共有しない
- 1 node 1 証明書にする
- 秘密鍵流出時はその証明書を失効する
- token も同時にローテーションする

## 7. 証明書失効

クライアント端末が紛失したり、秘密鍵が漏れた場合は失効します。

例: `jellyfin-home.crt` を失効する。

```bash
cd pki/ca
openssl ca -config openssl.cnf -revoke ../clients/jellyfin-home.crt
openssl ca -config openssl.cnf -gencrl -out ../crl.pem
```

確認:

```bash
openssl crl -in ../crl.pem -noout -text
```

注意:

- `zwidyd` の現在実装では CRL 読み込みまでは未実装です
- それでも PKI 側で `crl.pem` を持っておくと、将来の実装拡張にそのまま繋がります
- 現時点では失効時に token 無効化も必ず併用してください

## 8. 鍵サイズとアルゴリズム

このガイドでは互換性優先で `RSA 4096` を使っています。

選択肢としては次も有力です。

- `RSA 3072`: 十分強く、4096 より軽い
- `ECDSA P-256`: 高速で鍵も小さい
- `Ed25519`: とても扱いやすいが、証明書運用の周辺ツールや環境差分を気にする場合あり

迷ったらまずは以下を推奨します。

- **現実的な推奨:** `RSA 3072` または `RSA 4096`
- **性能重視:** `ECDSA P-256`

Zwidy の最初の運用では、OpenVPN ライクにわかりやすく保守しやすい `RSA 4096` のままでも十分です。

## 9. 本番運用チェックリスト

- CA 鍵はサーバに置かない
- サーバ秘密鍵の権限は `600`
- クライアント秘密鍵の権限は `600`
- サーバ証明書に `subjectAltName` を入れる
- サーバ証明書に `extendedKeyUsage = serverAuth` を入れる
- クライアント証明書に `extendedKeyUsage = clientAuth` を入れる
- node ごとに個別証明書と個別 token を使う
- 漏えい時のために失効手順を用意する
- 期限切れ前に更新手順を試す
- `zwidyd validate --config ...` をデプロイ前に実行する

## 10. まずはこれだけやればよい最小手順

急ぎで始めるだけなら、最低限次の順番で進めてください。

1. CA を作る
2. SAN 付きサーバ証明書を作る
3. node ごとのクライアント証明書を作る
4. クライアントに `ca.crt` を配る
5. `tls.server_name` を証明書 SAN と一致させる
6. token も併用する

## 11. 将来的により良い方法

運用対象が増えるなら、OpenSSL 手打ちより次のどちらかが楽です。

- `easy-rsa`: OpenVPN 文脈で馴染みがあり、個人〜小規模運用に向く
- `step-ca`: API と自動化がしやすく、将来的な短命証明書運用に向く

ただ、Zwidy の現段階では依存を増やさず理解しやすいことも重要なので、**まずは OpenSSL ベースのシンプルな自前 CA 運用が一番バランスが良い**と考えています。

## 参考

- OpenSSL `x509v3_config`: `subjectAltName` や `extendedKeyUsage` の設定
- OpenSSL `openssl-verify`: 証明書検証の考え方
- OpenSSL `openssl-ca`: 自前 CA と失効リスト運用

## 12. 半自動スクリプト

手作業を減らしたい場合は `scripts/gen-pki.sh` を使えます。

```bash
chmod +x ./scripts/gen-pki.sh
```

### CA 初期化

```bash
./scripts/gen-pki.sh init-ca --pki-dir ./pki --ca-cn "Zwidy Internal CA"
```

### サーバ証明書発行

```bash
./scripts/gen-pki.sh issue-server \
  --pki-dir ./pki \
  --name tunnel.example.com \
  --dns tunnel.example.com \
  --ip 203.0.113.10
```

### クライアント証明書発行

```bash
./scripts/gen-pki.sh issue-client \
  --pki-dir ./pki \
  --name jellyfin-home
```

### 失効と CRL 生成

```bash
./scripts/gen-pki.sh revoke-client --pki-dir ./pki --name jellyfin-home
./scripts/gen-pki.sh gen-crl --pki-dir ./pki
```

### 検証

```bash
./scripts/gen-pki.sh verify-server --pki-dir ./pki --name tunnel.example.com
./scripts/gen-pki.sh verify-client --pki-dir ./pki --name jellyfin-home
```

### 出力配置

- CA: `pki/ca/`
- サーバ証明書: `pki/server/<name>.crt`
- サーバ秘密鍵: `pki/server/<name>.key`
- クライアント証明書: `pki/clients/<name>.crt`
- クライアント秘密鍵: `pki/clients/<name>.key`
- CRL: `pki/crl.pem`

### 運用メモ

- `issue-server` は `--dns` か `--ip` を最低1つ要求します
- `verify-server` は SAN と EKU の確認に使えます
- `verify-client` は clientAuth の確認に使えます
- 秘密鍵のパーミッションはスクリプト側で `600` に設定します
- 既存 CA がある状態で `init-ca` を再実行すると上書き事故防止のため失敗します
