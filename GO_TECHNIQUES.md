# Go 1BRC Implementation Techniques

This repository includes a Go implementation in `src/main/go/calculate_average.go` plus the normal 1BRC runner pair:

```bash
./prepare_golang.sh
./calculate_average_golang.sh
./test.sh golang
```

The goal is to compete with the fastest Java-style solutions by keeping the hot path close to raw memory processing. It does not rely on Go's convenient `bufio.Scanner`, `strings.Split`, `strconv.ParseFloat`, or `map[string]...` in the row-processing loop.

## 1. Native binary instead of JVM or native image tuning

Go already builds a native executable, so there is no JVM startup, tiered compilation, JIT warmup, or GraalVM native-image step. `prepare_golang.sh` builds a stripped binary:

```bash
go build -trimpath -ldflags="-s -w" -o target/go/calculate_average_golang ./src/main/go
```

On AMD64, the script defaults `GOAMD64=v3` when the user has not already set `GOAMD64`. That lets the Go compiler target a newer x86-64 baseline on modern benchmark machines while still allowing overrides such as `GOAMD64=v4` or `GOAMD64=v2`.

## 2. Memory-mapped input

The program maps `measurements.txt` with `syscall.Mmap` and reads the file as one contiguous `[]byte`.

This avoids:

- repeated `read` syscalls;
- copying data through user-space buffers;
- line object creation;
- UTF-8 decoding in the hot path.

The mapping is intentionally not unmapped before exit. This is a short-lived benchmark process, and the operating system reclaims the mapping when the process exits. Explicit unmapping would add cleanup work to the timed path.

## 3. Newline-aligned chunks

The mapped file is split into byte ranges. Each range end is moved forward to the next newline so no worker starts or stops in the middle of a row.

Chunking is based on `GOMAXPROCS * 32`, giving more chunks than workers. This keeps workers balanced when some chunks contain longer station names or worse hash-table collision patterns.

## 4. Goroutine workers with atomic chunk claiming

Workers claim the next chunk through one atomic counter. There are no channels in the hot path. Channels are ergonomic, but their synchronization and queueing overhead are unnecessary for this workload.

Worker count defaults to `runtime.GOMAXPROCS(0)` and can be overridden:

```bash
BRC_WORKERS=8 ./calculate_average_golang.sh
```

## 5. Worker-local aggregation

Each worker owns a private hash table. The row-processing loop performs no shared hash-map writes, no locks, no atomics per row, and no cache-line bouncing between workers.

Only after all parsing completes does the program merge the worker-local tables into one final table.

## 6. Custom fixed-size hash table

The challenge has about 10,000 unique stations. The Go implementation uses an open-addressed table with `1 << 15` slots per worker.

Each table entry stores:

```text
hash, file offset, name length, min, max, sum, count
```

The station name bytes stay in the mmap data. The table stores only offsets and lengths. Collisions are resolved by linear probing and final byte equality checks.

This avoids the generic costs of `map[string]Stats`:

- string creation per row;
- generic hash-map control flow;
- pointer-heavy buckets;
- extra allocations;
- global-map synchronization.

## 7. Raw station-name handling

The parser never creates a `string` for an input row. A station key is represented as:

```text
data[offset : offset+length]
```

Only the final unique stations are written as text. Sorting also compares the raw UTF-8 bytes directly from the mmap region.

## 8. SWAR semicolon search

The station-name delimiter is found with an 8-byte SWAR scan. Each loop loads one `uint64`, XORs it with eight semicolon bytes, and uses the classic zero-byte test:

```text
(x - 0x0101010101010101) & ~x & 0x8080808080808080
```

When the mask is non-zero, `bits.TrailingZeros64(mask) / 8` gives the delimiter byte index.

This reduces branch-heavy byte-by-byte scanning for the station-name field.

## 9. Unsafe unaligned 64-bit loads

`load64` uses `unsafe.Pointer` to read a `uint64` from the mapped byte slice. This avoids helper-call overhead and lets both delimiter scanning and hashing operate one machine word at a time.

This is intentionally architecture-conscious. The target leaderboard class is modern 64-bit Linux hardware where unaligned 64-bit loads are supported efficiently enough for this workload.

## 10. Word-at-a-time station hashing

Station names are hashed in 8-byte pieces. The hash is not cryptographic; it is designed to spread about 10,000 station names well in a power-of-two table. Byte equality is still checked on every candidate match, so hash collisions cannot corrupt results.

## 11. Fixed-point temperature parsing

Temperatures are parsed as tenths of a degree:

```text
12.3  ->  123
-4.5  ->  -45
```

The parser handles only the valid 1BRC formats:

```text
N.N
NN.N
-N.N
-NN.N
```

This avoids `strconv.ParseFloat`, floating-point work in the hot loop, and temporary substrings.

## 12. Integer stats

The hot-loop update is integer-only:

```text
min = min(min, value)
max = max(max, value)
sum += value
count++
```

Average formatting happens once per unique station at the end.

## 13. Java-compatible average rounding

The original challenge expects Java-style one-decimal rounding. Go's `math.Round` rounds halfway cases away from zero, while Java's `Math.round` is equivalent to `floor(x + 0.5)`.

The implementation uses:

```go
int64(math.Floor(v + 0.5))
```

for average tenths to match Java output behavior.

## 14. Buffered final output

The program writes the final result through a 1 MiB `bufio.Writer`. There are only about 10,000 unique stations, so final formatting is not the main bottleneck, but buffering keeps output predictable and avoids many small writes.

## 15. Optional runtime knobs

Useful benchmark knobs:

```bash
GOMAXPROCS=8 BRC_WORKERS=8 ./calculate_average_golang.sh
GOGC=off ./calculate_average_golang.sh
GOAMD64=v4 ./prepare_golang.sh
```

`GOGC=off` should only be used after confirming the hot path is effectively allocation-free. If future changes allocate per row, disabling GC can make memory usage explode.

## What is not used yet

This implementation does not currently include hand-written assembly SIMD. For an attempt to beat the absolute fastest Java entries, the next likely step is AMD64 assembly for delimiter search and possibly station-name comparison. Pure Go can get close, but Go does not currently expose a stable high-level SIMD API comparable to Java's Vector API.

The implementation also does not special-case the known official station list. It remains a general 1BRC parser for valid input files.
