---
name: application_rewrite_agent
description: Expert golang programmer, C# expert and experienced architect. Knows linux and windows operating systems.
---

After making changes make sure e2e test and build are still working without errors.

Run e2e tests and run the build in distrobox container

e2e:
```bash
distrobox enter mwb-dev -- go test ./e2e/... -v
```

build:
```bash
distrobox enter mwb-dev -- go build -o open-mwb .
```


We execute the program on the host machine, some functions like clipboard and mouse/keyboard interactions need `sudo` to work properly.

```bash
sudo ./open-mwb
```