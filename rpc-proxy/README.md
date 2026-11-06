# rpc-proxy

Reference for the logging JSON-RPC proxy (handoff section 5.3, W11; demo beat 6). The proxy shows what an RPC provider sees. It logs the source IP, method and parameters of each accepted call to memory, then forwards the call to a dedicated Sepolia endpoint. To deploy it, see `deploy/rpc-proxy/README.md`.

## Properties

- **Holds no secrets.** The upstream URL comes from `UPSTREAM_RPC_URL` at start. The proxy never logs it or puts it in an error message. It does not relay the upstream body as received. It parses it and rebuilds each response from `jsonrpc`, `id` and `result`, or from `error.code`, a fixed `error.message` of `upstream error`, and `error.data` only if it is a hex string. Any other upstream field is dropped. The proxy answers HTTP 502 when the body is not JSON-typed, is not valid JSON-RPC, or is a `result` or `id` string that contains the upstream URL, its path, or a path segment, query value or credential of 8 or more characters (after JSON and percent decoding). It does not follow redirects. This is a check on those exact strings, not proof that a provider cannot leak a credential in another form. A short credential (under 8 characters) in a `result` is not detected. The proxy forwards no client headers.
- **Logs to memory only.** The log is a fixed-size ring. It is lost on restart. The source imports no filesystem module (a test checks this).
- **Forwards `eth_*`, `net_*` and `web3_*` methods only.** The proxy logs any other method and rejects it with HTTP 403.

## Endpoints

| Method and path         | Purpose                                                                         |
| ----------------------- | ------------------------------------------------------------------------------- |
| `POST /` or `POST /rpc` | JSON-RPC 2.0 call or batch. Logs, then forwards. Returns the upstream response. |
| `GET /log`              | Read-only log view (JSON). CORS: any origin. Query: `since`, `limit`.           |
| `GET /healthz`          | Returns `{"ok":true}`.                                                          |

`GET /log` returns:

```json
{
  "entries": [
    { "seq": 1, "time": "2026-11-06T10:00:00.000Z", "sourceIp": "203.0.113.5", "method": "eth_call", "params": "[...]" }
  ],
  "lastSeq": 1,
  "dropped": 0
}
```

- `since=N` returns entries with `seq` greater than N. Poll with the previous `lastSeq`.
- `limit=N` returns at most the newest N entries.
- `dropped` counts entries the ring evicted since start.
- A batch request produces one entry per call. A body that parses as JSON but is not an object with a string `method` produces one entry with method `(invalid)`. The proxy does not check the `jsonrpc` or `id` fields; the upstream does.
- Not logged: requests over the rate limit (HTTP 429) and bodies over `MAX_BODY_BYTES` (HTTP 413). A batch over `MAX_BATCH_SIZE` and an unparseable body produce one `(invalid)` entry, not one per call. Params longer than `MAX_LOGGED_PARAMS_CHARS` are truncated in the log.

## Environment variables

| Variable                  | Default     | Meaning                                                                                                                                                                     |
| ------------------------- | ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `UPSTREAM_RPC_URL`        | (required)  | Dedicated Sepolia endpoint. May contain an API key. Never commit it.                                                                                                        |
| `HOST`                    | `127.0.0.1` | Listen address.                                                                                                                                                             |
| `PORT`                    | `8545`      | Listen port.                                                                                                                                                                |
| `TRUSTED_PROXY_HOPS`      | `0`         | Reverse proxies in front that append to `X-Forwarded-For`. With 0, the proxy ignores that header and logs the socket address. With N, it logs the Nth entry from the right. |
| `LOG_CAPACITY`            | `2000`      | Entries kept in memory.                                                                                                                                                     |
| `MAX_BODY_BYTES`          | `65536`     | Larger bodies get HTTP 413.                                                                                                                                                 |
| `MAX_BATCH_SIZE`          | `20`        | Larger batches get HTTP 400.                                                                                                                                                |
| `MAX_LOGGED_PARAMS_CHARS` | `2048`      | Longer params are truncated in the log, not in the forwarded call.                                                                                                          |
| `RATE_LIMIT_PER_MINUTE`   | `300`       | Per source IP. 0 disables. Over the limit, HTTP 429.                                                                                                                        |
| `UPSTREAM_TIMEOUT_MS`     | `15000`     | An upstream timeout returns HTTP 502.                                                                                                                                       |

To run and test the proxy, see `deploy/rpc-proxy/README.md`.
