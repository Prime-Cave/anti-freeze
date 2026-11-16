# Build log: deviations and measurements

A running record of where the build differs from the original brief, and why.

* **Go version floor is 1.26**, not 1.23: required by Wails v3.1.0.
* **Vite 6** (not 7/8): Vite 7+ requires Node 20.19+; 6.4 supports any Node 20.
* **Menu bar icon bridge**: see ARCHITECTURE.md, "Gaps in Wails v3".
