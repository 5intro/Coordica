# Third-party notices

Coordica 的自定义许可只适用于 Coordica 自有代码和文档。第三方组件保留各自的版权和许可；Coordica 关于个人使用、非商业用途、团队使用或修改版发布的限制，不限制第三方许可已经授予的权利。下列许可文件必须随 Coordica 二进制发行包一同保留。

Coordica's custom license applies only to Coordica's own code and documentation. Third-party components remain subject to their respective licenses. No personal-use, noncommercial-use, team-use, or redistribution restriction in Coordica's license limits rights granted independently by those third-party licenses. Preserve this notice and the accompanying `third_party/licenses/` directory when redistributing a permitted Coordica package.

## Audit scope

This inventory records the `0.1.0-dev` release inputs. Go module versions were checked against `go.mod`, `go.sum`, and the build information in the macOS arm64, Windows amd64, and Linux amd64 executables. Frontend versions were checked against `web/package-lock.json` and the installed packages used for the production build. The files under [`third_party/licenses/`](third_party/licenses/) are copied verbatim from those upstream distributions; [`manifest.json`](third_party/licenses/manifest.json) records their SHA-256 hashes and source paths used during the audit.

The tables cover the union of components linked on the three release platforms. A component may be absent from one platform's binary. Development and test tools that are neither shipped nor embedded are not represented as application runtime dependencies. Vite and Rolldown notices are retained because the generated browser bundle includes build-generated runtime helper code.

## Go modules

The source links below identify the exact, unmodified upstream Go module source archives. Versions and module checksums are also pinned in [`go.sum`](go.sum).

| Component | Version | License | Preserved notices | Upstream source |
| --- | --- | --- | --- | --- |
| github.com/dustin/go-humanize | v1.0.1 | MIT | [LICENSE](third_party/licenses/go/github.com/dustin/go-humanize@v1.0.1/LICENSE) | [Module archive](https://proxy.golang.org/github.com/dustin/go-humanize/@v/v1.0.1.zip) |
| github.com/google/uuid | v1.6.0 | BSD-3-Clause | [LICENSE](third_party/licenses/go/github.com/google/uuid@v1.6.0/LICENSE) | [Module archive](https://proxy.golang.org/github.com/google/uuid/@v/v1.6.0.zip) |
| github.com/mattn/go-isatty | v0.0.24 | MIT | [LICENSE](third_party/licenses/go/github.com/mattn/go-isatty@v0.0.24/LICENSE) | [Module archive](https://proxy.golang.org/github.com/mattn/go-isatty/@v/v0.0.24.zip) |
| github.com/ncruces/go-strftime | v1.0.0 | MIT | [LICENSE](third_party/licenses/go/github.com/ncruces/go-strftime@v1.0.0/LICENSE) | [Module archive](https://proxy.golang.org/github.com/ncruces/go-strftime/@v/v1.0.0.zip) |
| github.com/remyoudompheng/bigfft | v0.0.0-20230129092748-24d4a6f8daec | BSD-3-Clause | [LICENSE](third_party/licenses/go/github.com/remyoudompheng/bigfft@v0.0.0-20230129092748-24d4a6f8daec/LICENSE) | [Module archive](https://proxy.golang.org/github.com/remyoudompheng/bigfft/@v/v0.0.0-20230129092748-24d4a6f8daec.zip) |
| golang.org/x/sys | v0.48.0 | BSD-3-Clause | [LICENSE](third_party/licenses/go/golang.org/x/sys@v0.48.0/LICENSE) | [Module archive](https://proxy.golang.org/golang.org/x/sys/@v/v0.48.0.zip) |
| modernc.org/libc | v1.77.1 | BSD-3-Clause; inherited notices below | [LICENSE](third_party/licenses/go/modernc.org/libc@v1.77.1/LICENSE), [third-party notices](third_party/licenses/go/modernc.org/libc@v1.77.1/LICENSE-3RD-PARTY.md) | [Module archive](https://proxy.golang.org/modernc.org/libc/@v/v1.77.1.zip) |
| modernc.org/mathutil | v1.7.1 | BSD-3-Clause | [LICENSE](third_party/licenses/go/modernc.org/mathutil@v1.7.1/LICENSE), [mersenne notice](third_party/licenses/go/modernc.org/mathutil@v1.7.1/mersenne/LICENSE) | [Module archive](https://proxy.golang.org/modernc.org/mathutil/@v/v1.7.1.zip) |
| modernc.org/memory | v1.12.1 | BSD-3-Clause | [LICENSE](third_party/licenses/go/modernc.org/memory@v1.12.1/LICENSE), [Go notice](third_party/licenses/go/modernc.org/memory@v1.12.1/LICENSE-GO), [mmap-go notice](third_party/licenses/go/modernc.org/memory@v1.12.1/LICENSE-MMAP-GO) | [Module archive](https://proxy.golang.org/modernc.org/memory/@v/v1.12.1.zip) |
| modernc.org/sqlite | v1.60.1 | BSD-3-Clause; SQLite public domain | [LICENSE](third_party/licenses/go/modernc.org/sqlite@v1.60.1/LICENSE), [SQLite dedication](third_party/licenses/go/modernc.org/sqlite@v1.60.1/LICENSE-SQLITE), [full upstream notices](third_party/licenses/go/modernc.org/sqlite@v1.60.1/LICENSE-3RD-PARTY.md) | [Module archive](https://proxy.golang.org/modernc.org/sqlite/@v/v1.60.1.zip) |

The `modernc.org/libc` notice includes the upstream Go, musl libc, go-netdb, and NixOS/nixpkgs copyright and license texts. The SQLite code in the pinned driver is SQLite 3.53.4, dedicated to the public domain. No local modifications were made to these modules.

The complete `modernc.org/sqlite` upstream notice also describes its own test, generator, and optional components. Retaining that notice does **not** mean all of those components are linked into Coordica. In particular, Coordica does not import `modernc.org/sqlite/vec` or `modernc.org/sqlite/vfs`, does not ship the upstream sponsor logos or memory logo, and does not link the MPL-2.0 `github.com/hashicorp/golang-lru/v2` generator dependency. The actual linked modernc modules listed above use BSD-3-Clause licenses; no MPL-covered component was identified in these release binaries.

## Go runtime and standard library

The release binaries were built with Go 1.27.2 and contain its runtime and standard-library code. Their [BSD-3-Clause license](third_party/licenses/go-toolchain/go1.27.2/LICENSE) and [patent grant](third_party/licenses/go-toolchain/go1.27.2/PATENTS) are preserved. Go's vendored copies of `golang.org/x/crypto`, `golang.org/x/net`, `golang.org/x/sys`, and `golang.org/x/text` retain their [license and patent files](third_party/licenses/go-toolchain/go1.27.2/src/vendor/golang.org/x/). The compiler/toolchain itself is not included in the downloadable Coordica packages.

Upstream source: [Go 1.27.2 source archive](https://go.dev/dl/go1.27.2.src.tar.gz).

## Embedded browser application

| Component | Version | License | Preserved notices | Upstream source |
| --- | --- | --- | --- | --- |
| React | 19.3.0 | MIT | [LICENSE](third_party/licenses/npm/react@19.3.0/LICENSE) | [npm archive](https://registry.npmjs.org/react/-/react-19.3.0.tgz) |
| React DOM | 19.3.0 | MIT | [LICENSE](third_party/licenses/npm/react-dom@19.3.0/LICENSE) | [npm archive](https://registry.npmjs.org/react-dom/-/react-dom-19.3.0.tgz) |
| Scheduler | 0.28.0 | MIT | [LICENSE](third_party/licenses/npm/scheduler@0.28.0/LICENSE) | [npm archive](https://registry.npmjs.org/scheduler/-/scheduler-0.28.0.tgz) |
| Vite generated runtime | 8.3.4 | MIT; full upstream notices preserved | [LICENSE.md](third_party/licenses/npm/vite@8.3.4/LICENSE.md) | [npm archive](https://registry.npmjs.org/vite/-/vite-8.3.4.tgz) |
| Rolldown generated helpers | 1.2.13 | MIT; full upstream notices preserved | [LICENSE](third_party/licenses/npm/rolldown@1.2.13/LICENSE) | [npm archive](https://registry.npmjs.org/rolldown/-/rolldown-1.2.13.tgz) |

The Vite and Rolldown license files also include notices for dependencies of the build tools. These complete upstream notices are retained without editing; they are not a claim that every build-tool dependency is present in the browser application.

## External programs

Git, Codex, Claude Code, dsh, their model services, and Node.js are not bundled with Coordica. Users install and use them separately under their own applicable licenses and service terms. The sample dsh MCP overlay is configuration for invoking the Coordica executable; it does not include the dsh executable or its MCP-client plugin implementation.

## Updating this inventory

Whenever dependencies, the Go toolchain, target platforms, or embedded browser assets change, review the resulting binary build information and production bundle again. Update the version table and source links, copy the corresponding upstream notices without altering their terms, and include this file and `third_party/licenses/` in every release archive.
