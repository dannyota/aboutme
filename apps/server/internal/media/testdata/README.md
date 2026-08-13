# Media normalization fixtures

`generate.go` creates the deterministic JPEG and PNG boundary fixtures. The
normalization benchmark manifest pins their hashes.

The two WebP decoder-regression fixtures are copied byte-for-byte from
`golang.org/x/image@v0.45.0/testdata`, the exact module version in
`apps/server/go.mod`:

- `blue-purple-pink-large.lossless.webp`
- `blue-purple-pink-large.no-filter.lossy.webp`

They are distributed under the Go project's BSD-3-Clause license, already
present in the module source used to build and test this project. Their hashes
in the manifest prevent silent replacement.
