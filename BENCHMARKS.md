# Benchmarks

Canopy ships Go benchmarks for the TUI and daemon hot paths. They run entirely
offline — no live Claude account or daemon process required.

## Running

```sh
make bench
```

Or directly:

```sh
go test -bench=. -benchmem ./internal/...
```

## Benchmarks

| Package | Benchmark | What it measures |
|---------|-----------|-----------------|
| `daemon` | `BenchmarkRenderTermANSI` | ANSI encoding of a vt10x cell grid |
| `daemon` | `BenchmarkSnapshotChanged` | Full snapshot render (the common active-session path) |
| `daemon` | `BenchmarkSnapshotUnchanged` | Snapshot render when revision matches (shows render still runs; cost of the current gate) |
| `tui` | `BenchmarkBuildTree` | Tree rebuild for 10 projects × 3 worktrees × 5 sessions |
| `tui` | `BenchmarkFilterTreeItems` | Workspace search over the same fixture |
| `tui` | `BenchmarkModelView` | Full TUI render pass with PTY content in the right panel |

> **Note:** these are synthetic micro-benchmarks of local CPU work. They do not
> reflect real Claude refresh rates, IPC latency, or daemon throughput.

## Comparing before and after

Install `benchstat`:

```sh
go install golang.org/x/perf/cmd/benchstat@latest
```

Capture a baseline on the unmodified branch, then capture results after your
change, and compare:

```sh
# on the baseline branch
go test -bench=. -benchmem -count=5 ./internal/... | tee /tmp/before.txt

# after your change
go test -bench=. -benchmem -count=5 ./internal/... | tee /tmp/after.txt

# compare
benchstat /tmp/before.txt /tmp/after.txt
```

`benchstat` applies statistical noise reduction across the five runs and flags
regressions with a `p`-value. A delta below ±2% is typically noise on a
developer machine; look for consistent directional changes across runs.

## Debug telemetry

Set the `--debug` flag (or call `tui.EnableDebug()`) to write per-request
telemetry to `/tmp/canopy-debug.log`:

```
[TUI] snapshot request sess=<id> since=<rev>
[TUI] snapshot response sess=<id> changed=true rev=42 bytes=8192 latency=312µs
[TUI] snapshot discarded sess=<id> changed=false active=<other-id>
[TUI] render 0.81ms
[SNAP] changed sess=<id> rev=42 bytes=8192
[SNAP] unchanged sess=<id> rev=42
```

Telemetry is off by default and has no effect on production performance.
