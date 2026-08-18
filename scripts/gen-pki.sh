#!/usr/bin/env bash
set -euo pipefail

SCRIPT_NAME=$(basename "$0")
DEFAULT_PKI_DIR=${ZWIDY_PKI_DIR:-./pki}
DEFAULT_CA_CN=${ZWIDY_CA_CN:-Zwidy Internal CA}
DEFAULT_KEY_BITS=${ZWIDY_KEY_BITS:-4096}
DEFAULT_CA_DAYS=${ZWIDY_CA_DAYS:-3650}
DEFAULT_CERT_DAYS=${ZWIDY_CERT_DAYS:-825}

usage() {
  cat <<USAGE
Usage:
  $SCRIPT_NAME init-ca [--pki-dir DIR] [--ca-cn NAME]
  $SCRIPT_NAME issue-server --name NAME [--dns DNS]... [--ip IP]... [--pki-dir DIR]
  $SCRIPT_NAME issue-client --name NAME [--pki-dir DIR]
  $SCRIPT_NAME revoke-client --name NAME [--pki-dir DIR]
  $SCRIPT_NAME gen-crl [--pki-dir DIR]
  $SCRIPT_NAME verify-server --name NAME [--pki-dir DIR]
  $SCRIPT_NAME verify-client --name NAME [--pki-dir DIR]
  $SCRIPT_NAME print-paths [--pki-dir DIR]

Examples:
  $SCRIPT_NAME init-ca --pki-dir ./pki --ca-cn "Zwidy Internal CA"
  $SCRIPT_NAME issue-server --name tunnel.example.com --dns tunnel.example.com --ip 203.0.113.10
  $SCRIPT_NAME issue-client --name jellyfin-home
  $SCRIPT_NAME revoke-client --name jellyfin-home
USAGE
}

die() {
  echo "error: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

abs_dir() {
  local input=$1
  mkdir -p "$input"
  (
    cd "$input"
    pwd
  )
}

ensure_ca_exists() {
  [[ -f "$CA_DIR/ca.key" ]] || die "CA key not found: $CA_DIR/ca.key"
  [[ -f "$CA_DIR/ca.crt" ]] || die "CA certificate not found: $CA_DIR/ca.crt"
  [[ -f "$CA_DIR/openssl.cnf" ]] || die "OpenSSL config not found: $CA_DIR/openssl.cnf"
}

write_ca_config() {
  cat > "$CA_DIR/openssl.cnf" <<CNF
[ ca ]
default_ca = CA_default

[ CA_default ]
dir               = $CA_DIR
certs             = \$dir
new_certs_dir     = \$dir
crl_dir           = \$dir
certificate       = \$dir/ca.crt
private_key       = \$dir/ca.key
database          = \$dir/index.txt
serial            = \$dir/serial
crlnumber         = \$dir/crlnumber
crl               = $PKI_DIR/crl.pem
default_md        = sha256
default_days      = $DEFAULT_CERT_DAYS
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
default_bits       = $DEFAULT_KEY_BITS
default_md         = sha256
prompt             = no
distinguished_name = dn
x509_extensions    = v3_ca

[ dn ]
CN = $CA_CN

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
CNF
}

write_server_req_config() {
  local name=$1
  shift
  local dns_names=("$@")
  local san_lines=()
  local idx=1
  local entry
  for entry in "${SERVER_DNS[@]}"; do
    san_lines+=("DNS.$idx = $entry")
    idx=$((idx + 1))
  done
  idx=1
  for entry in "${SERVER_IPS[@]}"; do
    san_lines+=("IP.$idx = $entry")
    idx=$((idx + 1))
  done
  [[ ${#san_lines[@]} -gt 0 ]] || die "at least one --dns or --ip is required"

  cat > "$SERVER_DIR/$name.cnf" <<CNF
[ req ]
default_bits       = $DEFAULT_KEY_BITS
prompt             = no
default_md         = sha256
distinguished_name = dn
req_extensions     = req_ext

[ dn ]
CN = $name

[ req_ext ]
subjectAltName = @alt_names

[ alt_names ]
$(printf '%s
' "${san_lines[@]}")
CNF
}

write_client_req_config() {
  local name=$1
  cat > "$CLIENTS_DIR/$name.cnf" <<CNF
[ req ]
default_bits       = $DEFAULT_KEY_BITS
prompt             = no
default_md         = sha256
distinguished_name = dn

[ dn ]
CN = $name
CNF
}

init_ca() {
  mkdir -p "$CA_DIR" "$SERVER_DIR" "$CLIENTS_DIR"
  : > "$CA_DIR/index.txt"
  printf '1000\n' > "$CA_DIR/serial"
  printf '1000\n' > "$CA_DIR/crlnumber"
  write_ca_config
  if [[ -f "$CA_DIR/ca.key" || -f "$CA_DIR/ca.crt" ]]; then
    die "CA files already exist under $CA_DIR"
  fi
  openssl genrsa -out "$CA_DIR/ca.key" "$DEFAULT_KEY_BITS"
  chmod 600 "$CA_DIR/ca.key"
  openssl req -x509 -new -nodes \
    -key "$CA_DIR/ca.key" \
    -sha256 \
    -days "$DEFAULT_CA_DAYS" \
    -out "$CA_DIR/ca.crt" \
    -config "$CA_DIR/openssl.cnf"
  echo "CA initialized under $PKI_DIR"
}

issue_server() {
  [[ -n "$NAME" ]] || die "--name is required"
  ensure_ca_exists
  mkdir -p "$SERVER_DIR"
  write_server_req_config "$NAME" "${SERVER_DNS[@]}"
  openssl genrsa -out "$SERVER_DIR/$NAME.key" "$DEFAULT_KEY_BITS"
  chmod 600 "$SERVER_DIR/$NAME.key"
  openssl req -new \
    -key "$SERVER_DIR/$NAME.key" \
    -out "$SERVER_DIR/$NAME.csr" \
    -config "$SERVER_DIR/$NAME.cnf"
  openssl ca -batch \
    -config "$CA_DIR/openssl.cnf" \
    -extensions server_cert \
    -in "$SERVER_DIR/$NAME.csr" \
    -out "$SERVER_DIR/$NAME.crt"
  openssl verify -CAfile "$CA_DIR/ca.crt" "$SERVER_DIR/$NAME.crt"
  echo "server certificate issued: $SERVER_DIR/$NAME.crt"
}

issue_client() {
  [[ -n "$NAME" ]] || die "--name is required"
  ensure_ca_exists
  mkdir -p "$CLIENTS_DIR"
  write_client_req_config "$NAME"
  openssl genrsa -out "$CLIENTS_DIR/$NAME.key" "$DEFAULT_KEY_BITS"
  chmod 600 "$CLIENTS_DIR/$NAME.key"
  openssl req -new \
    -key "$CLIENTS_DIR/$NAME.key" \
    -out "$CLIENTS_DIR/$NAME.csr" \
    -config "$CLIENTS_DIR/$NAME.cnf"
  openssl ca -batch \
    -config "$CA_DIR/openssl.cnf" \
    -extensions client_cert \
    -in "$CLIENTS_DIR/$NAME.csr" \
    -out "$CLIENTS_DIR/$NAME.crt"
  openssl verify -CAfile "$CA_DIR/ca.crt" "$CLIENTS_DIR/$NAME.crt"
  echo "client certificate issued: $CLIENTS_DIR/$NAME.crt"
}

revoke_client() {
  [[ -n "$NAME" ]] || die "--name is required"
  ensure_ca_exists
  [[ -f "$CLIENTS_DIR/$NAME.crt" ]] || die "client certificate not found: $CLIENTS_DIR/$NAME.crt"
  openssl ca -config "$CA_DIR/openssl.cnf" -revoke "$CLIENTS_DIR/$NAME.crt"
  gen_crl
  echo "client certificate revoked: $CLIENTS_DIR/$NAME.crt"
}

gen_crl() {
  ensure_ca_exists
  openssl ca -config "$CA_DIR/openssl.cnf" -gencrl -out "$PKI_DIR/crl.pem"
  echo "CRL generated: $PKI_DIR/crl.pem"
}

verify_server() {
  [[ -n "$NAME" ]] || die "--name is required"
  ensure_ca_exists
  openssl verify -CAfile "$CA_DIR/ca.crt" "$SERVER_DIR/$NAME.crt"
  openssl x509 -in "$SERVER_DIR/$NAME.crt" -noout -text | sed -n '/Subject Alternative Name/,+2p;/Extended Key Usage/,+1p'
}

verify_client() {
  [[ -n "$NAME" ]] || die "--name is required"
  ensure_ca_exists
  openssl verify -CAfile "$CA_DIR/ca.crt" "$CLIENTS_DIR/$NAME.crt"
  openssl x509 -in "$CLIENTS_DIR/$NAME.crt" -noout -text | sed -n '/Subject:/p;/Extended Key Usage/,+1p'
}

print_paths() {
  cat <<PATHS
PKI root:      $PKI_DIR
CA cert:       $CA_DIR/ca.crt
CA key:        $CA_DIR/ca.key
Server dir:    $SERVER_DIR
Clients dir:   $CLIENTS_DIR
CRL:           $PKI_DIR/crl.pem
PATHS
}

COMMAND=${1:-}
if [[ -z "$COMMAND" ]]; then
  usage
  exit 1
fi
if [[ "$COMMAND" == "-h" || "$COMMAND" == "--help" ]]; then
  usage
  exit 0
fi
shift || true

PKI_DIR=$DEFAULT_PKI_DIR
CA_CN=$DEFAULT_CA_CN
NAME=
declare -a SERVER_DNS=()
declare -a SERVER_IPS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pki-dir)
      [[ $# -ge 2 ]] || die "--pki-dir requires a value"
      PKI_DIR=$2
      shift 2
      ;;
    --ca-cn)
      [[ $# -ge 2 ]] || die "--ca-cn requires a value"
      CA_CN=$2
      shift 2
      ;;
    --name)
      [[ $# -ge 2 ]] || die "--name requires a value"
      NAME=$2
      shift 2
      ;;
    --dns)
      [[ $# -ge 2 ]] || die "--dns requires a value"
      SERVER_DNS+=("$2")
      shift 2
      ;;
    --ip)
      [[ $# -ge 2 ]] || die "--ip requires a value"
      SERVER_IPS+=("$2")
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

require_cmd openssl
PKI_DIR=$(abs_dir "$PKI_DIR")
CA_DIR="$PKI_DIR/ca"
SERVER_DIR="$PKI_DIR/server"
CLIENTS_DIR="$PKI_DIR/clients"

case "$COMMAND" in
  init-ca)
    init_ca
    ;;
  issue-server)
    issue_server
    ;;
  issue-client)
    issue_client
    ;;
  revoke-client)
    revoke_client
    ;;
  gen-crl)
    gen_crl
    ;;
  verify-server)
    verify_server
    ;;
  verify-client)
    verify_client
    ;;
  print-paths)
    print_paths
    ;;
  *)
    usage
    die "unknown command: $COMMAND"
    ;;
esac
