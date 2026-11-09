# Synthetic official-writer compatibility fixture

`upload-artifact-v7.zip.base64` contains only `evidence.json` with `{"synthetic":true,"format":"upload-artifact-v7"}`. It contains no package source, user artifact, credential or provider data.

Generated locally with the actual `createZipUploadStream` implementation from public `@actions/artifact@6.3.1`, compression level 6, with the versions in the pinned action's lockfile: archiver 7.0.1, zip-stream 6.0.1, compress-commons 6.0.2. Temporary public dependencies were installed with no credentials and lifecycle scripts disabled; they are not dependencies of this repository. The fixture exercises the real streamed DEFLATE/data-descriptor layout, not a Python approximation. ZIP64 was not used for this small entry; oversized/ZIP64 inputs remain rejected.

Official pin: [actions/upload-artifact v7.0.2 package lock](https://github.com/actions/upload-artifact/blob/cf430e030ddbb5b0abf93d22962f4752f3646cd9/package-lock.json). This establishes writer compatibility, not successful GitHub upload/download service integration.
