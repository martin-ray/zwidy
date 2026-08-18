# zwidy
Zwidy — a lightweight reverse tunnel for exposing private origins to CDN edge networks.
The name comes from the Japanese word 隧道 (zuidō), meaning “tunnel.”

## Current implementation

- Single binary daemon: `zwidyd`
- Commands: `server`, `client`, `validate`, `version`
- YAML-based configuration using the provided example files
- TLS-encrypted TCP transport
- Linux TUN interface setup and route management
- Stable server-side IP allocation persisted to disk
- Client authentication via per-node enrollment token
- Structured logging, `/metrics`, and `/healthz`

## Quick start

```bash
go build ./cmd/zwidyd
./zwidyd validate --config ./configs/server.example.yaml
```

The `quic` transport is accepted by config validation because it is part of the v1 spec, but runtime support is not implemented yet. Use `transport: tcp` for now.

## Operations

- PKI / certificate guide: `docs/pki.md`
- PKI helper script: `scripts/gen-pki.sh`
- Manual verification guide: `docs/manual-test.md`
