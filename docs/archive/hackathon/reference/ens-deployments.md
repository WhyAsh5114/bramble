# ENS deployment configuration

`BRAMBLE_ENS_DEPLOYMENT` selects the Sepolia contract set in `sidecar/src/ens/config.ts`. The node sidecar, admin CLI, approval service, relay sidecar and provisioning scripts use this module. The chain ID stays `11155111`.

| Value | Deployment | Default |
| --- | --- | --- |
| `hackathon-test` | September hackathon test deployment; existing tailnet | Yes, until the new lifecycle passes |
| `sepolia-v2` | Official ENSv2 Sepolia deployment, October 1, 2026 | No; opt in before starting the process |

An empty value uses the default. An unknown value stops startup. Export the selector before invoking a command; set it in each service's environment file. Generated node configs carry it as `ensDeployment`. Existing node configs without that field select `hackathon-test`.

## Official contract set

Read from source, November 7, 2026: the [ENS deployment docs](https://docs.ens.domains/learn/deployments/) link the deployed ABIs to [contracts-v2 revision `07e55a0`](https://github.com/ensdomains/contracts-v2/blob/07e55a056f5b6a9c90119f501bdd05714e67dddd/contracts/docs/addresses/sepolia.md). Its deployment timestamp is `2026-10-01T07:55:02.016Z`. Mutable branch address lists can describe a different deployment; use the immutable revision.

| Contract | Address |
| --- | --- |
| ETHRegistry | `0xd4ebcbbdf463c9c45784603db0ddd499bc44a8b4` |
| ETHRegistrar | `0xf633e7fc17e2bbe0d0965d18ec1821dcb754a3d3` |
| MockUSDC | `0x240b0316df57887dbbe58b586508b19e633a14aa` |
| VerifiableFactory | `0xda70306c98e97ece36f997a21368e53298572991` |
| PermissionedResolverImpl | `0x115eb53f0c60696633855f90b138178fb40b2b2c` |
| UserRegistryImpl | `0x9bd8a88719068d09ecee662f36c0e3856708366a` |
| UpgradableUniversalResolverProxy | `0xeeeeeeee14d718c2b47d9923deab1335e144eeee` |

The resolver proxy is the public resolution entry point. `UniversalResolverV2` at `0x24e1d8e068620b647ca097f961a61055f4f42d72` is its implementation, not the configured entry point.

Verified on Sepolia, November 7, 2026: all seven configured contracts have bytecode. The registrar's `ETH_REGISTRY()` returns the configured ETHRegistry. `MIN_COMMITMENT_AGE()` is 60 seconds; `MIN_REGISTER_DURATION()` is 2,419,200 seconds.

## ABI and roles

Read from source at the immutable revision: parent `register` takes label, owner, secret, subregistry, resolver, duration, payment token and referrer. Subregistry `register` takes label, owner, subregistry, resolver, role bitmap and expiry. The resolver initializer takes `Grant[]` plus `bytes[]`; the user registry initializer takes `Grant[]` alone.

`getState` returns the static tuple `(status, expiry, latestOwner, tokenId, resource)`; the existing flattened read ABI has the same wire encoding. `setText(bytes,string,string)` and `grantSetterRoles(bytes,address)` retain their signatures. `ROLE_REGISTRAR = 1 << 0` is a root registry role. `ROLE_SET_TEXT = 1 << 4` supports the independent per-setter permissions used by ADR 0004 and ADR 0005. No role or record-schema change is required for this deployment.

## Environment consistency

These values describe one deployment and must move together:

- `BRAMBLE_ENS_DEPLOYMENT`
- `BRAMBLE_TAILNET_NAME`, `BRAMBLE_TAILNET_REGISTRY`, `BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK`
- `BRAMBLE_RELAY_REGISTRY`, `BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK`
- Approval's `APPROVAL_ENS_PARENT`, equal to `BRAMBLE_TAILNET_NAME`

Contract selection does not translate records between deployments. Retain the old values for rollback. The provisioning command's `--parent-only --name <label>` mode emits the new public parent configuration and saves it to ignored `.tmp/<deployment>-parent.json`. It does not overwrite `sidecar/test/fixtures/dev-tailnet.json` or enroll a placeholder device.

## Migration parent

Verified November 7, 2026: `bramble-mumbai-20261107.eth` is registered on the official deployment and points to device registry `0xF9c3cEa16cD75A1E8efBEE89fF0C1c04aB3F4148` (deployment block `12078393`). Parent registration: [Sepolia transaction](https://sepolia.etherscan.io/tx/0x59cfd583af3b8548f5fc66ac55479b1bc8d92195ce045f27cad7ebd134b3d81e).

The enrolled identities are `mumbai-gateway`, `mumbai-worker` and `mumbai-granter2`. Their published WireGuard keys and mesh addresses match the existing demo identities. The gateway trusts `mumbai-granter2`; the worker begins with an empty ACL.

The separate relay registry is `0x94F99d10eda494aF86D7d6E8431Ecd189E97412d` (deployment block `12078414`). `mumbai-relay.relays.bramble-mumbai-20261107.eth` carries the existing relay's public rendezvous, sidecar and price records.

## Cutover (verified November 7, 2026)

The public console, installer (release `a29809f`), approval service, gateway and worker run on `bramble-mumbai-20261107.eth` on the official deployment. A full worker lifecycle passed there: denied by the gateway, grant written to the registry in 15 s, successful call after 44 s, revoke confirmed in 19 s, refusal on the next call.

On the cloud nodes, `/etc/systemd/system/bramble-{gateway,worker}.service.d/ensv2.conf` loads `/etc/bramble/ensv2-stage.env` over the original environment. To roll back, delete both drop-ins, run `systemctl daemon-reload` and restart the units; the original environment files were not changed. The approval service reads the new values from its environment file; its pre-cutover copy is `approval.env.bak-pre-ensv2-cutover`.

The repository default deployment stays `hackathon-test`; the public installer selects `sepolia-v2` through its served `bramble.json`.

Open: a fresh device installing on macOS has not been run.
