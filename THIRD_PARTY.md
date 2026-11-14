# Third-party code

| Component | Version | License | Where | Why |
|---|---|---|---|---|
| [UAParser.js](https://github.com/faisalman/ua-parser-js) | 1.0.41 | MIT ([internal/uaparser/LICENSE-ua-parser-js.md](internal/uaparser/LICENSE-ua-parser-js.md)) | `internal/uaparser/ua-parser.js`, unmodified, run in goja | The device check parses User-Agents as the Node.js gateway does. Its sha256 is checked by `TestEmbeddedSourceIsPinned`. |
