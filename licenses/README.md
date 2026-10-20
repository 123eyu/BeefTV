# License texts and media source inputs

`GPL-2.0.txt`, `GPL-3.0.txt`, and `LGPL-2.1.txt` are unmodified copies of
`COPYING.GPLv2`, `COPYING.GPLv3`, and `COPYING.LGPLv2.1` from FFmpeg commit
`894da5ca7d742e4429ffb2af534fcda0103ef593`:
https://github.com/FFmpeg/FFmpeg/tree/894da5ca7d742e4429ffb2af534fcda0103ef593

`distribution-inputs.json` owns the reviewed source packet selection for
prebuilt binaries. `ffmpeg-wasm-sources.json` records candidate upstream
source inputs for rebuilding the WASM core; its unresolved status must not
be promoted to a historical binary attestation.

See [media source distribution](../docs/media-source-distribution.md) for
the packet format, collection, final artifact checks and publication rules.
`source-packets/` is an ignored container build input, not a source archive
stored in Git.
