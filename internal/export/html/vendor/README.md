# Vendored vis-timeline

*Audience: anyone updating the timeline library the HTML export embeds.*

- **Library:** [vis-timeline](https://github.com/visjs/vis-timeline) 8.5.4
- **File:** `vis-timeline-graph2d.min.js` is the package's
  `standalone/umd/vis-timeline-graph2d.min.js`, which bundles moment and injects its own
  CSS, with its last line, the `//# sourceMappingURL=` comment, removed so browser
  devtools request no source map.
- **SHA-256 of the committed file:**
  `022a9091d0237035f8b9f2ef622dc10b24ab4304d2a5629e748e073883b857dc`
- **Licence:** `Apache-2.0 OR MIT`, at the user's choice; the package's texts are
  [`LICENSE-APACHE-2.0.txt`](LICENSE-APACHE-2.0.txt) and
  [`LICENSE-MIT.txt`](LICENSE-MIT.txt).

## Updating

Run this with `sh` from this directory and the new version, then update the version and
SHA-256 above. It stops at the first failure, including a download whose integrity does
not match the registry's.

```sh
set -eu
version=8.5.4
curl -fsSL "https://registry.npmjs.org/vis-timeline/-/vis-timeline-$version.tgz" -o vis.tgz
want=$(curl -fsS "https://registry.npmjs.org/vis-timeline/$version" | jq -r .dist.integrity)
[ "$want" = "sha512-$(openssl dgst -sha512 -binary vis.tgz | openssl base64 -A)" ] || { echo "integrity mismatch" >&2; exit 1; }
tar -xzf vis.tgz
sed '/^\/\/# sourceMappingURL=/d' package/standalone/umd/vis-timeline-graph2d.min.js >vis-timeline-graph2d.min.js
cp package/LICENSE.Apache-2.0.txt LICENSE-APACHE-2.0.txt
cp package/LICENSE.MIT.txt LICENSE-MIT.txt
rm -r package vis.tgz
if grep -qi -e '</script' -e '<!--' vis-timeline-graph2d.min.js; then echo "holds </script or <!--" >&2; exit 1; fi
shasum -a 256 vis-timeline-graph2d.min.js
```

The `grep` ignores case and stops the block when the file holds `</script` or `<!--`: the
page inlines the file in a `<script>` element, which either would break. Then run
`go test ./internal/export`, which fails until the SHA-256 above matches the file, and
open an exported page in a browser to check the timeline still renders with no network
requests.

Two parts of `viewer.js` lean on 8.5.4's internals, and an upgrade that breaks either
still renders a page. It sets the groups again after the first `changed` event, because
8.5.4 first clusters in a redraw too late to restack the rows, which left clustered events
hidden until the window moved. And its `clusterCriteria` keeps what it accepts contiguous:
`getClusters` sorts a row by centre, a range's midpoint, collects the next items that pass
the criterion against the cluster's first item, and resumes by a count, not a position, so
a criterion that skips an item inside a run starts a second cluster on an existing member
and counts it twice. After an upgrade, check that a row of dense events clusters on first
open without moving the window, and whether it still does with the `setGroups` call
removed; and, with a range among points in one row, that no event is drawn both alone and
inside a cluster.
