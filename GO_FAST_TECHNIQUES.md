# Fast Go 1BRC Candidate

This document explains the second Go implementation in `src/main/go-fast/calculate_average.go`.

Run it with the normal 1BRC fork naming convention:

```bash
./prepare_golang_fast.sh
./calculate_average_golang_fast.sh
./test.sh golang_fast
```

The implementation is written as a serious attempt to compete with the fastest Java solutions. It uses Go as a native systems language: mmap input, raw byte parsing, worker-local aggregation, custom hash tables, unsafe word loads, SWAR delimiter detection, and no per-row allocations. Beating the absolute best Java entry still has to be proven on the official Linux AMD64 benchmark machine, but the code is structured for that target.

## 1. Separate Fast Candidate

The earlier Go implementation remains in `src/main/go`. The faster candidate lives in `src/main/go-fast` so correctness and performance experiments do not disturb the simpler version.

The runner name is `golang_fast`, so the existing test harness works unchanged:

```bash
./test.sh golang_fast
```

## 2. Native Go Binary

Go builds directly to a native executable. There is no JVM startup, JIT warmup, tiered compilation, or GraalVM native-image preparation.

`prepare_golang_fast.sh` builds a stripped binary:

```bash
go build -trimpath -ldflags="-s -w" -o target/go/calculate_average_golang_fast ./src/main/go-fast
```

On AMD64, it defaults to `GOAMD64=v3` unless the environment already sets `GOAMD64`. Benchmark runs can try `GOAMD64=v4` on newer machines.

## 3. Mmap With Kernel Advice

The input file is mapped with `syscall.Mmap` and read as one continuous `[]byte`.

The implementation also calls:

```go
syscall.Madvise(data, syscall.MADV_SEQUENTIAL)
```

The benchmark input is usually on a RAM disk, so this is not a magic switch. It is still a useful hint that the program will scan the mapping sequentially.

The program does not explicitly unmap before exit. It is a short-lived batch process, and process teardown reclaims the mapping without adding timed cleanup work.

## 4. Newline-Aligned Chunking

The file is split into byte ranges, and every chunk end is moved to a newline. That guarantees no worker parses a partial row.

The chunk count is `GOMAXPROCS * 32`. More chunks than workers gives load balancing when station names, hash collisions, or cache behavior vary by region of the input.

## 5. Atomic Work Stealing Without Channels

Workers claim chunk indexes from one atomic counter. There are no channels in the parse path.

Channels are useful for general Go programs, but here one atomic increment per chunk is cheaper and enough.

## 6. Worker-Local Hash Tables

Each worker owns its hash table. During parsing there are:

- no shared map writes;
- no locks;
- no per-row atomics;
- no concurrent map overhead;
- no cache-line bouncing for station updates.

Tables are merged only after all workers finish.

## 7. Larger Low-Load-Factor Table

The fast candidate uses `1 << 16` slots per worker. With about 10,000 unique stations, this keeps the table load low and reduces linear-probing length.

The memory tradeoff is intentional. On an 8-worker run, the tables use tens of MiB, which is small compared with the official 128 GiB machine and the input file.

## 8. Single-Pass Station Scan And Hash

The previous Go version found `;` and then scanned the station name again to hash it.

The fast candidate combines these operations. `scanName` reads the station field one `uint64` at a time, searches for `;`, and updates the station hash while it scans.

This removes one full pass over every station name.

## 9. SWAR Semicolon Search

Delimiter search uses the classic 8-byte zero-byte trick:

```text
(x - 0x0101010101010101) & ~x & 0x8080808080808080
```

The code XORs each loaded word with `0x3b3b3b3b3b3b3b3b`, so any semicolon becomes a zero byte. `bits.TrailingZeros64(mask) / 8` gives the delimiter position.

This cuts down branch-heavy byte scanning.

## 10. Unsafe Unaligned Word Loads

`load64` reads `uint64` values directly from the mmap-backed byte slice with `unsafe.Pointer`.

The target is modern 64-bit Linux hardware where unaligned 64-bit loads are efficient enough for this workload. This is not written as portable Windows code; it is written for the 1BRC benchmark class.

## 11. Station Fingerprints

Each hash-table entry stores:

```text
hash, first 8 bytes, second 8 bytes, last 8 bytes, offset, length, stats
```

For names up to 24 bytes, those three fingerprints cover the whole station name. Matching entries do not need `bytes.Equal`.

For names longer than 24 bytes, fingerprints reject almost all non-matches quickly, then an exact byte comparison preserves correctness.

## 12. No String Creation In The Hot Path

The parser never creates a `string` for a row. Station keys are offsets and lengths into the mmap data.

Strings are not needed for final output either: the writer emits the original raw bytes for each unique station. Sorting also compares byte slices directly.

## 13. Fixed-Point Temperature Parser

Temperatures are parsed as tenths of a degree:

```text
12.3  ->  123
-4.5  ->  -45
```

Only the valid 1BRC formats are handled:

```text
N.N
NN.N
-N.N
-NN.N
```

The hot loop does not call `strconv.ParseFloat`, allocate substrings, or do floating-point math.

## 14. Integer-Only Java Rounding

The output average must match Java's `Math.round`, which is equivalent to:

```text
floor(x + 0.5)
```

The fast candidate avoids floating-point conversion and computes the rounded average in integer arithmetic:

```go
numerator := sum*2 + int64(count)
denominator := int64(count) * 2
```

For negative numerators, it performs floor division rather than Go's default truncation toward zero.

## 15. Buffered Output

Final output uses a 1 MiB `bufio.Writer`. There are only about 10,000 stations, but buffering keeps output overhead predictable.

## 16. Runtime Knobs

Useful benchmark commands:

```bash
GOMAXPROCS=8 BRC_WORKERS=8 ./calculate_average_golang_fast.sh
GOGC=off ./calculate_average_golang_fast.sh
GOAMD64=v4 ./prepare_golang_fast.sh
```

`GOGC=off` is only appropriate if the implementation remains allocation-free in the hot path.

## 17. Remaining Path To Beat The Best Java

The most plausible next optimization is AMD64 assembly SIMD for delimiter search and station-name comparison. Pure Go can be highly competitive, but Go does not currently provide a stable high-level SIMD API like Java's Vector API.

This implementation deliberately avoids official-station-list special casing, so it remains a general 1BRC parser for valid inputs.
